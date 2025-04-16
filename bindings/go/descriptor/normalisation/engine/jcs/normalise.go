package jcs

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

// Normalise is a helper that prepares and marshals a normalised value.
func Normalise(v interface{}, ex ExcludeRules) ([]byte, error) {
	entries, err := PrepareNormalisation(Type, v, ex)
	if err != nil {
		return nil, err
	}
	return entries.Marshal("")
}

// Type is the default normalisation instance.
var Type = normalisation{}

// normalisation implements the Normalisation interface using JCS (RFC 8785).
type normalisation struct{}

// New returns a new normalisation instance.
func New() Normalisation {
	return normalisation{}
}

// NewArray creates a new normalised array.
func (_ normalisation) NewArray() Normalised {
	return &normalised{value: make([]interface{}, 0)}
}

// NewMap creates a new normalised map.
func (_ normalisation) NewMap() Normalised {
	return &normalised{value: make(map[string]interface{})}
}

// NewValue wraps a basic value into a normalised value.
func (_ normalisation) NewValue(v interface{}) Normalised {
	return &normalised{value: v}
}

// String returns a descriptive name for this normalisation.
func (_ normalisation) String() string {
	return "JCS(rfc8785) normalisation"
}

// normalised is a wrapper for values undergoing normalisation.
type normalised struct {
	value interface{}
}

// Value returns the underlying value.
func (n *normalised) Value() interface{} {
	return n.value
}

// IsEmpty checks whether the normalised value is empty (for maps and arrays).
func (n *normalised) IsEmpty() bool {
	switch v := n.value.(type) {
	case map[string]interface{}:
		return len(v) == 0
	case []interface{}:
		return len(v) == 0
	default:
		return false
	}
}

// Append adds an element to a normalised array.
func (n *normalised) Append(elem Normalised) {
	n.value = append(n.value.([]interface{}), elem.Value())
}

// SetField sets a field in a normalised map.
func (n *normalised) SetField(name string, value Normalised) {
	n.value.(map[string]interface{})[name] = value.Value()
}

// toString recursively formats a value with indentation.
func toString(v interface{}, gap string) string {
	if v == nil || v == Null {
		return "null"
	}
	switch casted := v.(type) {
	case map[string]interface{}:
		ngap := gap + "  "
		s := "{"
		// Use ordered keys to ensure consistent output.
		keys := OrderedKeys(casted)
		for _, key := range keys {
			s += fmt.Sprintf("\n%s  %s: %s", gap, key, toString(casted[key], ngap))
		}
		return s + "\n" + gap + "}"
	case []interface{}:
		ngap := gap + "  "
		s := "["
		for _, elem := range casted {
			s += fmt.Sprintf("\n%s%s", ngap, toString(elem, ngap))
		}
		return s + "\n" + gap + "]"
	case string:
		return casted
	case bool:
		return strconv.FormatBool(casted)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprintf("%v", casted)
	default:
		panic(fmt.Sprintf("unknown type %T in toString; this should not happen", v))
	}
}

// ToString returns a string representation of the normalised value with the given indentation.
func (n *normalised) ToString(gap string) string {
	return toString(n.value, gap)
}

// String returns the JSON marshaled string of the normalised value.
func (n *normalised) String() string {
	data, err := json.Marshal(n.value)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// Formatted returns an indented JSON string of the normalised value.
func (n *normalised) Formatted() string {
	data, err := json.MarshalIndent(n.value, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(data)
}

// Marshal encodes the normalised value to JSON. If no indentation is requested,
// it applies JSON canonicalization.
func (n *normalised) Marshal(gap string) ([]byte, error) {
	buffer := new(bytes.Buffer)
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", gap)

	if err := encoder.Encode(n.Value()); err != nil {
		return nil, err
	}
	if gap != "" {
		return buffer.Bytes(), nil
	}
	// Canonicalize JSON if no indent is used.
	data, err := jsoncanonicalizer.Transform(buffer.Bytes())
	if err != nil {
		return nil, fmt.Errorf("cannot canonicalize json: %w", err)
	}
	return data, nil
}

// LabelExcludes defines exclusion rules for label entries during normalisation.
var LabelExcludes = ExcludeEmpty{
	ExcludeRules: DynamicArrayExcludes{
		ValueChecker: IgnoreLabelsWithoutSignature,
		Continue: MapIncludes{
			"name":    NoExcludes{},
			"version": NoExcludes{},
			"value":   NoExcludes{},
			"signing": NoExcludes{},
		},
	},
}

// IgnoreLabelsWithoutSignature checks if a label lacks a valid signature and should be ignored.
func IgnoreLabelsWithoutSignature(v interface{}) bool {
	if m, ok := v.(map[string]interface{}); ok {
		if sig, ok := m["signing"]; ok && sig != nil {
			return sig != "true" && sig != true
		}
	}
	return true
}

// MapIncludes defines inclusion rules for a map. Only the listed fields are included.
type MapIncludes map[string]ExcludeRules

// Field returns the inclusion rule for the given field.
func (r MapIncludes) Field(name string, value interface{}) (string, interface{}, ExcludeRules) {
	if rule, ok := r[name]; ok {
		if rule == nil {
			rule = NoExcludes{}
		}
		return name, value, rule
	}
	return "", nil, nil
}

// Element is not supported for MapIncludes.
func (r MapIncludes) Element(v interface{}) (bool, interface{}, ExcludeRules) {
	panic("invalid exclude structure, require array but found struct rules")
}

func (r MapIncludes) Filter(v Normalised) (Normalised, error) {
	return v, nil
}

// Constants for "none" access types.
const (
	NoneType       = "none"
	NoneLegacyType = "None"
)

// MapResourcesWithNoneAccess maps resources with "none" access, removing the digest.
func MapResourcesWithNoneAccess(v interface{}) interface{} {
	return MapResourcesWithAccessType(
		IsNoneAccessKind,
		func(v interface{}) interface{} {
			m := v.(map[string]interface{})
			delete(m, "digest")
			return m
		},
		v,
	)
}

// IsNoneAccessKind checks if the given access type is "none".
func IsNoneAccessKind(kind string) bool {
	return kind == NoneType || kind == NoneLegacyType
}

// MapResourcesWithAccessType applies a mapper function if the access type matches.
func MapResourcesWithAccessType(test func(string) bool, mapper func(interface{}) interface{}, v interface{}) interface{} {
	access, ok := v.(map[string]interface{})["access"]
	if !ok || access == nil {
		return v
	}
	typ, ok := access.(map[string]interface{})["type"]
	if !ok || typ == nil {
		return v
	}
	if s, ok := typ.(string); ok && test(s) {
		return mapper(v)
	}
	return v
}

type MapValue struct {
	Mapping  ValueMapper
	Continue ExcludeRules
}

func (m MapValue) MapValue(value interface{}) interface{} {
	if m.Mapping != nil {
		return m.Mapping(value)
	}
	return value
}

func (m MapValue) Field(name string, value interface{}) (string, interface{}, ExcludeRules) {
	if m.Continue != nil {
		return m.Continue.Field(name, value)
	}
	return name, value, NoExcludes{}
}

func (m MapValue) Element(value interface{}) (bool, interface{}, ExcludeRules) {
	if m.Continue != nil {
		return m.Continue.Element(value)
	}
	return true, value, NoExcludes{}
}

func (m MapValue) Filter(v Normalised) (Normalised, error) {
	if m.Continue != nil {
		return m.Continue.Filter(v)
	}
	return v, nil
}

// ExcludeEmpty wraps exclusion rules and filters out empty normalised values.
type ExcludeEmpty struct {
	ExcludeRules
}

var (
	_ ExcludeRules        = ExcludeEmpty{}
	_ NormalisationFilter = ExcludeEmpty{}
)

// Field applies exclusion to a field; if no rule is set and the value is nil, the field is excluded.
func (e ExcludeEmpty) Field(name string, value interface{}) (string, interface{}, ExcludeRules) {
	if e.ExcludeRules == nil {
		if value == nil {
			return "", nil, e
		}
		return name, value, e
	}
	return e.ExcludeRules.Field(name, value)
}

// Element applies exclusion to an array element.
func (e ExcludeEmpty) Element(value interface{}) (bool, interface{}, ExcludeRules) {
	if e.ExcludeRules == nil {
		if value == nil {
			return true, nil, e
		}
		return false, value, e
	}
	return e.ExcludeRules.Element(value)
}

// Filter removes a normalised value if it is empty.
func (ExcludeEmpty) Filter(v Normalised) (Normalised, error) {
	if v == nil || v.IsEmpty() {
		return nil, nil
	}
	return v, nil
}

// DynamicArrayExcludes defines exclusion rules for arrays where each element is checked dynamically.
type DynamicArrayExcludes struct {
	ValueChecker ValueChecker // Checks if an element should be excluded.
	ValueMapper  ValueMapper  // Maps an element before applying further rules.
	Continue     ExcludeRules // Rules for further processing of the element.
}

type (
	// ValueMapper transforms a value.
	ValueMapper func(v interface{}) interface{}
	// ValueChecker determines if a value should be excluded.
	ValueChecker func(value interface{}) bool
)

var _ ExcludeRules = DynamicArrayExcludes{}

// Field is not applicable for DynamicArrayExcludes.
func (r DynamicArrayExcludes) Field(name string, value interface{}) (string, interface{}, ExcludeRules) {
	panic("invalid exclude structure, require struct but found array rules")
}

// Element applies dynamic exclusion rules to an array element.
func (r DynamicArrayExcludes) Element(value interface{}) (bool, interface{}, ExcludeRules) {
	// First check if the element should be excluded based on the ValueChecker
	exclude := r.ValueChecker != nil && r.ValueChecker(value)
	if exclude {
		return true, value, nil
	}

	// Apply value mapping if specified
	if r.ValueMapper != nil {
		value = r.ValueMapper(value)
	}

	// Return the processed value with continuation rules
	return false, value, r.Continue
}

func (r DynamicArrayExcludes) Filter(v Normalised) (Normalised, error) {
	return v, nil
}

// MapExcludes defines exclusion rules for map (struct) fields.
type MapExcludes map[string]ExcludeRules

var _ ExcludeRules = MapExcludes{}

// Field returns the exclusion rule for a map field.
func (r MapExcludes) Field(name string, value interface{}) (string, interface{}, ExcludeRules) {
	if rule, ok := r[name]; ok {
		if rule == nil {
			return "", nil, nil
		}
		return name, value, rule
	}
	return name, value, NoExcludes{}
}

// Element is not applicable for MapExcludes.
func (r MapExcludes) Element(value interface{}) (bool, interface{}, ExcludeRules) {
	panic("invalid exclude structure, require array but found struct rules")
}

// Filter removes a normalised value if it is empty.
func (r MapExcludes) Filter(v Normalised) (Normalised, error) {
	return v, nil
}

// NoExcludes means no exclusion should be applied.
type NoExcludes struct{}

var _ ExcludeRules = NoExcludes{}

// Field for NoExcludes returns the field unchanged.
func (r NoExcludes) Field(name string, value interface{}) (string, interface{}, ExcludeRules) {
	return name, value, r
}

// Element for NoExcludes returns the element unchanged.
func (r NoExcludes) Element(value interface{}) (bool, interface{}, ExcludeRules) {
	return false, value, r
}

// Filter removes a normalised value if it is empty.
func (r NoExcludes) Filter(v Normalised) (Normalised, error) {
	return v, nil
}

// ArrayExcludes defines exclusion rules for arrays.
type ArrayExcludes struct {
	Continue ExcludeRules // Rules to apply to each element.
}

var _ ExcludeRules = ArrayExcludes{}

// Field is not applicable for ArrayExcludes.
func (r ArrayExcludes) Field(name string, value interface{}) (string, interface{}, ExcludeRules) {
	panic("invalid exclude structure, require struct but found array rules")
}

// Element applies the continuation rule to an array element.
func (r ArrayExcludes) Element(value interface{}) (bool, interface{}, ExcludeRules) {
	return false, value, r.Continue
}

func (r ArrayExcludes) Filter(v Normalised) (Normalised, error) {
	return v, nil
}

// Normalisation defines methods to create normalised JSON structures.
type Normalisation interface {
	NewArray() Normalised
	NewMap() Normalised
	NewValue(v interface{}) Normalised
	String() string
}

// Normalised represents a normalised JSON structure.
type Normalised interface {
	Value() interface{}
	IsEmpty() bool
	Marshal(gap string) ([]byte, error)
	Append(Normalised)
	SetField(name string, value Normalised)
}

// ExcludeRules defines how to exclude fields or elements during normalisation.
type ExcludeRules interface {
	Field(name string, value interface{}) (string, interface{}, ExcludeRules)
	Element(v interface{}) (bool, interface{}, ExcludeRules)
	Filter(Normalised) (Normalised, error)
}

// ValueMappingRule allows a rule to transform a value before exclusion is applied.
type ValueMappingRule interface {
	MapValue(v interface{}) interface{}
}

// NormalisationFilter allows post-processing of a normalised structure.
type NormalisationFilter interface {
	Filter(Normalised) (Normalised, error)
}

// null implements Normalised for a null value.
type null struct{}

func (n *null) IsEmpty() bool                          { return true }
func (n *null) Marshal(gap string) ([]byte, error)     { return json.Marshal(nil) }
func (n *null) ToString(gap string) string             { return n.String() }
func (n *null) String() string                         { return "null" }
func (n *null) Formatted() string                      { return n.String() }
func (n *null) Append(normalised Normalised)           { panic("append on null") }
func (n *null) Value() interface{}                     { return nil }
func (n *null) SetField(name string, value Normalised) { panic("set field on null") }

// Null represents a normalised null value.
var Null Normalised = (*null)(nil)

// PrepareNormalisation converts an input value into a normalised structure,
// by marshalling it to JSON and then unmarshalling into a map or array.
func PrepareNormalisation(n Normalisation, v interface{}, excludes ExcludeRules) (Normalised, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	// Try to unmarshal as a map first
	var rawMap map[string]interface{}
	if err = json.Unmarshal(data, &rawMap); err == nil {
		return prepareStruct(n, rawMap, excludes)
	}

	// If that fails, try as an array
	var rawArray []interface{}
	if err = json.Unmarshal(data, &rawArray); err == nil {
		return prepareArray(n, rawArray, excludes)
	}

	// If both fail, try as a basic value
	return n.NewValue(v), nil
}

// Prepare recursively converts an input value into a normalised structure,
// applying any exclusion rules along the way.
func Prepare(n Normalisation, v interface{}, ex ExcludeRules) (Normalised, error) {
	if v == nil {
		return Null, nil
	}

	// Use NoExcludes if exclusion rules are nil
	if ex == nil {
		ex = NoExcludes{}
	}

	// If the exclusion rule supports value mapping, apply it.
	if mapper, ok := ex.(ValueMappingRule); ok {
		v = mapper.MapValue(v)
	}

	var result Normalised
	var err error
	switch typed := v.(type) {
	case map[string]interface{}:
		result, err = prepareStruct(n, typed, ex)
	case []interface{}:
		result, err = prepareArray(n, typed, ex)
	default:
		return n.NewValue(v), nil
	}
	if err != nil {
		return nil, err
	}
	// Apply any normalisation filter if available.
	if filter, ok := ex.(NormalisationFilter); ok {
		return filter.Filter(result)
	}
	return result, nil
}

// prepareStruct normalises a map by applying exclusion rules to each field.
func prepareStruct(n Normalisation, v map[string]interface{}, ex ExcludeRules) (Normalised, error) {
	if v == nil {
		return n.NewMap(), nil
	}

	// Use NoExcludes if exclusion rules are nil
	if ex == nil {
		ex = NoExcludes{}
	}

	entries := n.NewMap()
	if entries == nil {
		return nil, fmt.Errorf("failed to create new map")
	}

	for key, value := range v {
		if value == nil {
			continue
		}
		name, mapped, prop := ex.Field(key, value)
		if name != "" {
			nested, err := Prepare(n, mapped, prop)
			if err != nil {
				return nil, fmt.Errorf("field %q: %w", key, err)
			}
			if nested != nil {
				if nested == Null {
					entries.SetField(name, nil)
				} else {
					entries.SetField(name, nested)
				}
			}
		}
	}
	return entries, nil
}

// prepareArray normalises an array by applying exclusion rules to each element.
func prepareArray(n Normalisation, v []interface{}, ex ExcludeRules) (Normalised, error) {
	if v == nil {
		return n.NewArray(), nil
	}

	// Use NoExcludes if exclusion rules are nil
	if ex == nil {
		ex = NoExcludes{}
	}

	entries := n.NewArray()
	if entries == nil {
		return nil, fmt.Errorf("failed to create new array")
	}

	for index, value := range v {
		exclude, mapped, prop := ex.Element(value)
		if !exclude {
			nested, err := Prepare(n, mapped, prop)
			if err != nil {
				return nil, fmt.Errorf("entry %d: %w", index, err)
			}
			if nested != nil {
				entries.Append(nested)
			} else {
				// Preserve nil values in the array
				entries.Append(n.NewValue(nil))
			}
		}
	}
	return entries, nil
}

// OrderedKeys returns the sorted keys of a map for consistent ordering.
func OrderedKeys[M ~map[K]V, K cmp.Ordered, V any](m M) []K {
	keys := slices.Collect(maps.Keys(m))
	slices.Sort(keys)
	return keys
}
