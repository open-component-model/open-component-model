package jcs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

// Normalise is a helper function that prepares and marshals a normalized value.
// It takes an input value and exclusion rules, and returns the canonicalized JSON representation.
//
// Parameters:
//   - v: The input value to normalize
//   - ex: Exclusion rules to apply during normalization
//
// Returns:
//   - []byte: The canonicalized JSON representation
//   - error: Any error that occurred during normalization
func Normalise(v interface{}, ex ExcludeRules) ([]byte, error) {
	entries, err := PrepareNormalisation(Type, v, ex)
	if err != nil {
		return nil, err
	}
	return entries.Marshal("")
}

// Type is the default normalisation instance implementing the JCS algorithm.
var Type = normalisation{}

// normalisation implements the Normalisation interface using JCS (RFC 8785).
// It provides methods to create and work with normalized JSON structures.
type normalisation struct{}

// New returns a new normalisation instance.
// This allows creating multiple independent normalisation instances if needed.
func New() Normalisation {
	return normalisation{}
}

// NewArray creates a new normalized array.
// Returns a Normalised interface that can be used to build an array structure.
func (_ normalisation) NewArray() Normalised {
	return &normalised{value: make([]interface{}, 0)}
}

// NewMap creates a new normalized map.
// Returns a Normalised interface that can be used to build a map structure.
func (_ normalisation) NewMap() Normalised {
	return &normalised{value: make(map[string]interface{})}
}

// NewValue wraps a basic value into a normalized value.
// This is used for primitive types that don't need special handling.
func (_ normalisation) NewValue(v interface{}) Normalised {
	return &normalised{value: v}
}

// String returns a descriptive name for this normalisation.
func (_ normalisation) String() string {
	return "JCS(rfc8785) normalisation"
}

// normalised is a wrapper for values undergoing normalisation.
// It implements the Normalised interface and provides methods to work with
// normalized JSON structures.
type normalised struct {
	value interface{}
}

// Value returns the underlying value of the normalized structure.
func (n *normalised) Value() interface{} {
	return n.value
}

// IsEmpty checks whether the normalized value is empty.
// For maps and arrays, it checks if they have no elements.
// For other types, it always returns false.
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

// Append adds an element to a normalized array.
// Panics if called on a non-array value.
func (n *normalised) Append(elem Normalised) {
	n.value = append(n.value.([]interface{}), elem.Value())
}

// SetField sets a field in a normalized map.
// Panics if called on a non-map value.
func (n *normalised) SetField(name string, value Normalised) {
	n.value.(map[string]interface{})[name] = value.Value()
}

// toString recursively formats a value with indentation.
// This is used for debugging and pretty-printing purposes.
func toString(v interface{}, gap string) string {
	if v == nil || v == Null {
		return "null"
	}
	switch casted := v.(type) {
	case map[string]interface{}:
		ngap := gap + "  "
		s := "{"
		// Use ordered keys to ensure consistent output.
		for _, key := range slices.Sorted(maps.Keys(casted)) {
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

// ToString returns a string representation of the normalized value with the given indentation.
func (n *normalised) ToString(gap string) string {
	return toString(n.value, gap)
}

// String returns the JSON marshaled string of the normalized value.
func (n *normalised) String() string {
	data, err := json.Marshal(n.value)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// Formatted returns an indented JSON string of the normalized value.
func (n *normalised) Formatted() string {
	data, err := json.MarshalIndent(n.value, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(data)
}

// Marshal encodes the normalized value to JSON.
// If no indentation is requested, it applies JSON canonicalization.
//
// Parameters:
//   - gap: The indentation string to use. If empty, canonicalization is applied.
//
// Returns:
//   - []byte: The encoded JSON data
//   - error: Any error that occurred during encoding
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

// LabelExcludes defines exclusion rules for label entries during normalization.
// It excludes labels that don't have a valid signature.
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
// Returns true if the label should be excluded from normalization.
func IgnoreLabelsWithoutSignature(v interface{}) bool {
	if m, ok := v.(map[string]interface{}); ok {
		if sig, ok := m["signing"]; ok && sig != nil {
			return sig != "true" && sig != true
		}
	}
	return true
}

// Constants for "none" access types.
const (
	NoneType       = "none"
	NoneLegacyType = "None"
)

// MapResourcesWithNoneAccess maps resources with "none" access, removing the digest.
// This is used to handle special cases where resources with no access type
// should have their digest removed during normalization.
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
// Supports both current and legacy access type values.
func IsNoneAccessKind(kind string) bool {
	return kind == NoneType || kind == NoneLegacyType
}

// MapResourcesWithAccessType applies a mapper function if the access type matches.
// This is used to transform resources based on their access type.
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

// Normalisation defines methods to create normalized JSON structures.
// This interface is implemented by the normalisation type.
type Normalisation interface {
	NewArray() Normalised
	NewMap() Normalised
	NewValue(v interface{}) Normalised
	String() string
}

// Normalised represents a normalized JSON structure.
// It provides methods to work with the normalized data.
type Normalised interface {
	Value() interface{}
	IsEmpty() bool
	Marshal(gap string) ([]byte, error)
	Append(Normalised)
	SetField(name string, value Normalised)
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

// Null represents a normalized null value.
var Null Normalised = (*null)(nil)

// PrepareNormalisation converts an input value into a normalized structure,
// by marshaling it to JSON and then unmarshaling into a map or array.
// This is the main entry point for the normalization process.
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

// Prepare recursively converts an input value into a normalized structure,
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

	// Check if the value can be marshaled to JSON
	if _, err := json.Marshal(v); err != nil {
		return nil, fmt.Errorf("cannot marshal value: %w", err)
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

// prepareStruct normalizes a map by applying exclusion rules to each field.
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

// prepareArray normalizes an array by applying exclusion rules to each element.
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
