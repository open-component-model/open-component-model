package jcs

// TransformationRules defines how to transform fields or elements during normalization.
// Different implementations provide different transformation strategies.
// The interface provides methods to:
//   - Transform individual fields in a map/struct (Field)
//   - Transform elements in an array (Element)
//   - Apply post-processing filters to normalized values (Filter)
type TransformationRules interface {
	// Field transforms a field in a map/struct during normalization.
	// Parameters:
	//   - name: The name of the field
	//   - value: The value of the field
	// Returns:
	//   - string: The transformed field name (empty string to exclude the field)
	//   - interface{}: The transformed field value
	//   - TransformationRules: Rules to apply to nested structures
	Field(name string, value interface{}) (string, interface{}, TransformationRules)

	// Element transforms an element in an array during normalization.
	// Parameters:
	//   - v: The array element value
	// Returns:
	//   - bool: true if the element should be excluded, false otherwise
	//   - interface{}: The transformed element value
	//   - TransformationRules: Rules to apply to nested structures
	Element(v interface{}) (bool, interface{}, TransformationRules)

	// NormalisationFilter applies post-processing to a normalized value.
	NormalisationFilter
}

// ValueMappingRule allows a rule to transform a value before exclusion is applied.
// This is typically used in conjunction with TransformationRules to modify values
// before they are processed by exclusion rules.
type ValueMappingRule interface {
	// MapValue transforms a value during normalization.
	// Parameters:
	//   - v: The value to transform
	// Returns:
	//   - interface{}: The transformed value
	MapValue(v interface{}) interface{}
}

// NormalisationFilter allows post-processing of a normalized structure.
// This interface is used to apply final transformations or filtering to normalized values
// after the main normalization process is complete. It can be used to:
//   - Remove empty or invalid values
//   - Apply final transformations to the normalized structure
//   - Validate the normalized output
type NormalisationFilter interface {
	// Filter applies post-processing to a normalized value.
	// This method is called after the main normalization process and allows for
	// final modifications or validation of the normalized structure.
	// Parameters:
	//   - Normalised: The normalized value to filter
	// Returns:
	//   - Normalised: The filtered normalized value (nil if the value should be removed)
	//   - error: Any error that occurred during filtering
	Filter(Normalised) (Normalised, error)
}

// MapExcludes defines exclusion rules for map (struct) fields.
// It specifies which fields should be excluded from the normalized output.
// Any field not listed in the map is included by default, but can also be included explicitly using NoExcludes.
type MapExcludes map[string]TransformationRules

var _ TransformationRules = MapExcludes{}

// Field returns the exclusion rule for a map field.
func (r MapExcludes) Field(name string, value interface{}) (string, interface{}, TransformationRules) {
	if rule, ok := r[name]; ok {
		if rule == nil {
			return "", nil, nil
		}
		return name, value, rule
	}
	return name, value, NoExcludes{}
}

// Element is not applicable for MapExcludes as it's meant for map structures.
func (r MapExcludes) Element(value interface{}) (bool, interface{}, TransformationRules) {
	panic("invalid exclude structure, require array but found struct rules")
}

// Filter removes a normalized value if it is empty.
func (r MapExcludes) Filter(v Normalised) (Normalised, error) {
	return v, nil
}

// MapIncludes defines inclusion rules for a map.
// Only the listed fields are included in the normalized output.
type MapIncludes map[string]TransformationRules

// Field returns the inclusion rule for the given field.
// If the field is not in the inclusion list, it is excluded.
func (r MapIncludes) Field(name string, value interface{}) (string, interface{}, TransformationRules) {
	if rule, ok := r[name]; ok {
		if rule == nil {
			rule = NoExcludes{}
		}
		return name, value, rule
	}
	return "", nil, nil
}

// Element is not supported for MapIncludes as it's meant for map structures.
func (r MapIncludes) Element(v interface{}) (bool, interface{}, TransformationRules) {
	panic("invalid exclude structure, require array but found struct rules")
}

func (r MapIncludes) Filter(v Normalised) (Normalised, error) {
	return v, nil
}

// NoExcludes means no exclusion should be applied.
// This is used when all fields or elements should be included in the output.
type NoExcludes struct{}

var _ TransformationRules = NoExcludes{}

// Field for NoExcludes returns the field unchanged.
func (r NoExcludes) Field(name string, value interface{}) (string, interface{}, TransformationRules) {
	return name, value, r
}

// Element for NoExcludes returns the element unchanged.
func (r NoExcludes) Element(value interface{}) (bool, interface{}, TransformationRules) {
	return false, value, r
}

// Filter removes a normalized value if it is empty.
func (r NoExcludes) Filter(v Normalised) (Normalised, error) {
	return v, nil
}

// ArrayExcludes defines exclusion rules for arrays.
// It applies the same rules to all elements in the array.
type ArrayExcludes struct {
	Continue TransformationRules // Rules to apply to each element
}

var _ TransformationRules = ArrayExcludes{}

// Field is not applicable for ArrayExcludes as it's meant for array structures.
func (r ArrayExcludes) Field(name string, value interface{}) (string, interface{}, TransformationRules) {
	panic("invalid exclude structure, require struct but found array rules")
}

// Element applies the continuation rule to an array element.
func (r ArrayExcludes) Element(value interface{}) (bool, interface{}, TransformationRules) {
	return false, value, r.Continue
}

func (r ArrayExcludes) Filter(v Normalised) (Normalised, error) {
	return v, nil
}

// DynamicArrayExcludes defines exclusion rules for arrays where each element is checked dynamically.
// This allows for complex filtering of array elements based on their content.
type DynamicArrayExcludes struct {
	ValueChecker ValueChecker        // Checks if an element should be excluded
	ValueMapper  ValueMapper         // Maps an element before applying further rules
	Continue     TransformationRules // Rules for further processing of the element
}

type (
	// ValueMapper transforms a value during normalization.
	ValueMapper func(v interface{}) interface{}
	// ValueChecker determines if a value should be excluded from normalization.
	ValueChecker func(value interface{}) bool
)

var _ TransformationRules = DynamicArrayExcludes{}

// Field is not applicable for DynamicArrayExcludes as it's meant for array structures.
func (r DynamicArrayExcludes) Field(name string, value interface{}) (string, interface{}, TransformationRules) {
	panic("invalid exclude structure, require struct but found array rules")
}

// Element applies dynamic exclusion rules to an array element.
func (r DynamicArrayExcludes) Element(value interface{}) (bool, interface{}, TransformationRules) {
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

// ExcludeEmpty wraps exclusion rules and filters out empty normalized values.
// This is useful for removing empty maps and arrays from the output.
type ExcludeEmpty struct {
	TransformationRules
}

var (
	_ TransformationRules = ExcludeEmpty{}
	_ NormalisationFilter = ExcludeEmpty{}
)

// Field applies exclusion to a field; if no rule is set and the value is nil, the field is excluded.
func (e ExcludeEmpty) Field(name string, value interface{}) (string, interface{}, TransformationRules) {
	if e.TransformationRules == nil {
		if value == nil {
			return "", nil, e
		}
		return name, value, e
	}
	return e.TransformationRules.Field(name, value)
}

// Element applies exclusion to an array element.
func (e ExcludeEmpty) Element(value interface{}) (bool, interface{}, TransformationRules) {
	if e.TransformationRules == nil {
		if value == nil {
			return true, nil, e
		}
		return false, value, e
	}
	return e.TransformationRules.Element(value)
}

// Filter removes a normalized value if it is empty.
func (ExcludeEmpty) Filter(v Normalised) (Normalised, error) {
	if v == nil || v.IsEmpty() {
		return nil, nil
	}
	return v, nil
}

// MapValue allows mapping values before applying exclusion rules.
// This is useful for transforming values during normalization.
type MapValue struct {
	Mapping  ValueMapper         // Function to transform the value
	Continue TransformationRules // Rules to apply after mapping
}

// MapValue transforms a value using the provided mapping function.
func (m MapValue) MapValue(value interface{}) interface{} {
	if m.Mapping != nil {
		return m.Mapping(value)
	}
	return value
}

// Field applies the mapping and continuation rules to a map field.
func (m MapValue) Field(name string, value interface{}) (string, interface{}, TransformationRules) {
	if m.Continue != nil {
		return m.Continue.Field(name, value)
	}
	return name, value, NoExcludes{}
}

// Element applies the mapping and continuation rules to an array element.
func (m MapValue) Element(value interface{}) (bool, interface{}, TransformationRules) {
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
