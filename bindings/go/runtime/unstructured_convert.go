package runtime

import (
	"bytes"
	"encoding"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
)

var (
	jsonMarshalerType = reflect.TypeFor[json.Marshaler]()
	textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()
	numberType        = reflect.TypeFor[json.Number]()
)

// maxUnstructuredDepth guards against reference cycles, which are surfaced as an error
// instead of overflowing the stack.
const maxUnstructuredDepth = 10000

// toUnstructuredMap is a trimmed port of Kubernetes'
// runtime.DefaultUnstructuredConverter.ToUnstructured (k8s.io/apimachinery/pkg/runtime/converter.go),
// kept dependency-free; consult that implementation as the reference when extending this.
//
// toUnstructuredMap converts a Typed into a map[string]any, preserving concrete numeric types
// (int64/float64) rather than coercing everything to float64 as a json.Unmarshal into interface{}
// would. Its output is JSON-native and marshals identically to encoding/json. json.Marshaler and
// encoding.TextMarshaler are honoured (e.g. runtime.Type marshals to "name/version").
func toUnstructuredMap(from Typed) (map[string]any, error) {
	c := &unstructuredConverter{}
	v, err := c.value(reflect.ValueOf(from))
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected object when converting %T to unstructured, got %T", from, v)
	}
	return m, nil
}

type unstructuredConverter struct {
	depth int
}

func (c *unstructuredConverter) value(v reflect.Value) (any, error) {
	c.depth++
	defer func() { c.depth-- }()
	if c.depth > maxUnstructuredDepth {
		return nil, fmt.Errorf("maximum nesting depth %d exceeded while converting to unstructured (possible reference cycle)", maxUnstructuredDepth)
	}

	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil, nil
		}
		v = v.Elem()
	}

	// json.Number is a string type; keep it a concrete number.
	if v.Type() == numberType {
		return numberToUnstructured(json.Number(v.String()))
	}

	if out, ok, err := marshalerToUnstructured(v); ok || err != nil {
		return out, err
	}

	switch v.Kind() {
	case reflect.String:
		return v.String(), nil
	case reflect.Bool:
		return v.Bool(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		u := v.Uint()
		if u > math.MaxInt64 {
			// Too large for int64; json.Number is JSON-native and marshals as a bare number.
			return json.Number(strconv.FormatUint(u, 10)), nil
		}
		return int64(u), nil
	case reflect.Float32:
		return floatToUnstructured(v.Float(), 32)
	case reflect.Float64:
		return floatToUnstructured(v.Float(), 64)
	case reflect.Slice:
		if v.IsNil() {
			return nil, nil
		}
		if v.Type().Elem().Kind() == reflect.Uint8 { // []byte -> base64, like encoding/json
			return base64.StdEncoding.EncodeToString(v.Bytes()), nil
		}
		return c.slice(v)
	case reflect.Array:
		// [N]byte is an array of numbers, not base64 (matching encoding/json).
		return c.slice(v)
	case reflect.Map:
		if v.IsNil() {
			return nil, nil
		}
		return c.mapValue(v)
	case reflect.Struct:
		return c.structValue(v)
	default:
		return nil, fmt.Errorf("unsupported kind %s when converting to unstructured", v.Kind())
	}
}

// floatToUnstructured rejects NaN/Inf (no JSON form) and re-rounds a float32 through 32-bit
// shortest formatting so it marshals like encoding/json (float32(0.1) -> 0.1).
func floatToUnstructured(f float64, bitSize int) (any, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, fmt.Errorf("unsupported float value %v: NaN and Inf cannot be represented in JSON", f)
	}
	if bitSize == 32 {
		rounded, err := strconv.ParseFloat(strconv.FormatFloat(f, 'g', -1, 32), 64)
		if err != nil {
			return nil, fmt.Errorf("normalizing float32 value: %w", err)
		}
		return rounded, nil
	}
	return f, nil
}

// marshalerToUnstructured returns ok=false if v implements neither marshaler. Marshaler output
// is decoded with UseNumber so numbers it produces stay lossless.
func marshalerToUnstructured(v reflect.Value) (any, bool, error) {
	t := v.Type()

	if m, ok := asMarshaler[json.Marshaler](v, t, jsonMarshalerType); ok {
		data, err := m.MarshalJSON()
		if err != nil {
			return nil, true, fmt.Errorf("MarshalJSON failed for %s: %w", t, err)
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		var out any
		if err := dec.Decode(&out); err != nil {
			return nil, true, fmt.Errorf("decoding MarshalJSON output of %s: %w", t, err)
		}
		return out, true, nil
	}

	if m, ok := asMarshaler[encoding.TextMarshaler](v, t, textMarshalerType); ok {
		data, err := m.MarshalText()
		if err != nil {
			return nil, true, fmt.Errorf("MarshalText failed for %s: %w", t, err)
		}
		return string(data), true, nil
	}

	return nil, false, nil
}

// asMarshaler returns v as T if the type or its pointer implements iface. Like encoding/json, an
// addressable value prefers the pointer-receiver marshaler (it may shadow a promoted value one,
// e.g. descriptor/v2.Time); pointer-only marshalers on non-addressable values are copied to honour them.
func asMarshaler[T any](v reflect.Value, t reflect.Type, iface reflect.Type) (T, bool) {
	if v.CanAddr() && reflect.PointerTo(t).Implements(iface) {
		return v.Addr().Interface().(T), true
	}
	if t.Implements(iface) {
		return v.Interface().(T), true
	}
	if reflect.PointerTo(t).Implements(iface) {
		p := reflect.New(t)
		p.Elem().Set(v)
		return p.Interface().(T), true
	}
	var zero T
	return zero, false
}

func numberToUnstructured(n json.Number) (any, error) {
	if n == "" { // encoding/json encodes an empty json.Number as 0
		return int64(0), nil
	}
	if i, err := n.Int64(); err == nil {
		return i, nil
	}
	f, err := n.Float64()
	if err != nil {
		return nil, fmt.Errorf("invalid json.Number %q: %w", n.String(), err)
	}
	return f, nil
}

func (c *unstructuredConverter) slice(v reflect.Value) (any, error) {
	out := make([]any, v.Len())
	for i := range v.Len() {
		e, err := c.value(v.Index(i))
		if err != nil {
			return nil, err
		}
		out[i] = e
	}
	return out, nil
}

func (c *unstructuredConverter) mapValue(v reflect.Value) (any, error) {
	out := make(map[string]any, v.Len())
	iter := v.MapRange()
	for iter.Next() {
		key, err := mapKeyToString(iter.Key())
		if err != nil {
			return nil, err
		}
		val, err := c.value(iter.Value())
		if err != nil {
			return nil, err
		}
		out[key] = val
	}
	return out, nil
}

// mapKeyToString follows encoding/json: TextMarshaler keys first, then string, then integer kinds.
func mapKeyToString(k reflect.Value) (string, error) {
	if tm, ok := asMarshaler[encoding.TextMarshaler](k, k.Type(), textMarshalerType); ok {
		b, err := tm.MarshalText()
		if err != nil {
			return "", fmt.Errorf("marshaling map key of type %s: %w", k.Type(), err)
		}
		return string(b), nil
	}
	switch k.Kind() {
	case reflect.String:
		return k.String(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(k.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(k.Uint(), 10), nil
	default:
		return "", fmt.Errorf("unsupported map key type %s when converting to unstructured", k.Type())
	}
}

func (c *unstructuredConverter) structValue(v reflect.Value) (any, error) {
	t := v.Type()
	out := make(map[string]any, t.NumField())

	// Two passes so a shallower field wins over a promoted embedded one regardless of order:
	// keyed fields first, then inlined embedded structs without overwriting existing keys.
	var inlined []int
	for i := range t.NumField() {
		field := t.Field(i)
		if field.PkgPath != "" && !field.Anonymous { // unexported, non-embedded
			continue
		}
		tag := parseJSONTag(field)
		if tag.name == "-" {
			continue
		}

		// Anonymous (pointer-to-)struct without an explicit name is inlined; other anonymous
		// fields are keyed by name when exported, ignored when unexported (as in encoding/json).
		if field.Anonymous && tag.name == "" {
			if isStructType(field.Type) {
				inlined = append(inlined, i)
				continue
			}
			if field.PkgPath != "" {
				continue
			}
		}

		fv := v.Field(i)
		if tag.omitempty && isEmptyValue(fv) {
			continue
		}
		if tag.omitzero && isZeroForOmit(fv) {
			continue
		}
		name := tag.name
		if name == "" {
			name = field.Name
		}
		val, err := c.value(fv)
		if err != nil {
			return nil, err
		}
		if tag.quoted {
			if val, err = applyStringOption(val); err != nil {
				return nil, err
			}
		}
		out[name] = val
	}

	for _, i := range inlined {
		inner, err := c.value(v.Field(i))
		if err != nil {
			return nil, err
		}
		m, ok := inner.(map[string]any)
		if !ok {
			continue // nil embedded pointer
		}
		for k, val := range m {
			if _, exists := out[k]; !exists {
				out[k] = val
			}
		}
	}
	return out, nil
}

func isStructType(t reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Kind() == reflect.Struct
}

type jsonTag struct {
	name      string
	omitempty bool
	omitzero  bool
	quoted    bool // ",string"
}

func parseJSONTag(field reflect.StructField) jsonTag {
	raw, ok := field.Tag.Lookup("json")
	if !ok {
		return jsonTag{}
	}
	parts := strings.Split(raw, ",")
	tag := jsonTag{name: parts[0]}
	for _, opt := range parts[1:] {
		switch opt {
		case "omitempty":
			tag.omitempty = true
		case "omitzero":
			tag.omitzero = true
		case "string":
			tag.quoted = true
		}
	}
	return tag
}

// applyStringOption implements the ",string" option: a scalar field is stored as a quoted JSON
// string; other kinds are returned unchanged.
func applyStringOption(val any) (any, error) {
	switch val.(type) {
	case string, bool, int64, float64, json.Number:
		data, err := json.Marshal(val)
		if err != nil {
			return nil, fmt.Errorf("applying ,string option: %w", err)
		}
		return string(data), nil
	default:
		return val, nil
	}
}

func isZeroForOmit(v reflect.Value) bool {
	if v.CanInterface() {
		if z, ok := v.Interface().(interface{ IsZero() bool }); ok {
			return z.IsZero()
		}
	}
	if v.CanAddr() && v.Addr().CanInterface() {
		if z, ok := v.Addr().Interface().(interface{ IsZero() bool }); ok {
			return z.IsZero()
		}
	}
	return v.IsZero()
}

func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return v.IsNil()
	default:
		return false
	}
}
