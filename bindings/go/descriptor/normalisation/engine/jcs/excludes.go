package jcs

// ExcludeRules defines how to exclude fields or elements during normalization.
// Different implementations provide different exclusion strategies.
type ExcludeRules interface {
	Field(name string, value interface{}) (string, interface{}, ExcludeRules)
	Element(v interface{}) (bool, interface{}, ExcludeRules)
	Filter(Normalised) (Normalised, error)
}

// ValueMappingRule allows a rule to transform a value before exclusion is applied.
type ValueMappingRule interface {
	MapValue(v interface{}) interface{}
}

// NormalisationFilter allows post-processing of a normalized structure.
type NormalisationFilter interface {
	Filter(Normalised) (Normalised, error)
}

// MapExcludes defines exclusion rules for map (struct) fields.
// It specifies which fields should be excluded from the normalized output.
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

// Element is not applicable for MapExcludes as it's meant for map structures.
func (r MapExcludes) Element(value interface{}) (bool, interface{}, ExcludeRules) {
	panic("invalid exclude structure, require array but found struct rules")
}

// Filter removes a normalized value if it is empty.
func (r MapExcludes) Filter(v Normalised) (Normalised, error) {
	return v, nil
}

// MapIncludes defines inclusion rules for a map.
// Only the listed fields are included in the normalized output.
type MapIncludes map[string]ExcludeRules

// Field returns the inclusion rule for the given field.
// If the field is not in the inclusion list, it is excluded.
func (r MapIncludes) Field(name string, value interface{}) (string, interface{}, ExcludeRules) {
	if rule, ok := r[name]; ok {
		if rule == nil {
			rule = NoExcludes{}
		}
		return name, value, rule
	}
	return "", nil, nil
}

// Element is not supported for MapIncludes as it's meant for map structures.
func (r MapIncludes) Element(v interface{}) (bool, interface{}, ExcludeRules) {
	panic("invalid exclude structure, require array but found struct rules")
}

func (r MapIncludes) Filter(v Normalised) (Normalised, error) {
	return v, nil
}

// NoExcludes means no exclusion should be applied.
// This is used when all fields or elements should be included in the output.
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

// Filter removes a normalized value if it is empty.
func (r NoExcludes) Filter(v Normalised) (Normalised, error) {
	return v, nil
}

// ArrayExcludes defines exclusion rules for arrays.
// It applies the same rules to all elements in the array.
type ArrayExcludes struct {
	Continue ExcludeRules // Rules to apply to each element
}

var _ ExcludeRules = ArrayExcludes{}

// Field is not applicable for ArrayExcludes as it's meant for array structures.
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

// DynamicArrayExcludes defines exclusion rules for arrays where each element is checked dynamically.
// This allows for complex filtering of array elements based on their content.
type DynamicArrayExcludes struct {
	ValueChecker ValueChecker // Checks if an element should be excluded
	ValueMapper  ValueMapper  // Maps an element before applying further rules
	Continue     ExcludeRules // Rules for further processing of the element
}

type (
	// ValueMapper transforms a value during normalization.
	ValueMapper func(v interface{}) interface{}
	// ValueChecker determines if a value should be excluded from normalization.
	ValueChecker func(value interface{}) bool
)

var _ ExcludeRules = DynamicArrayExcludes{}

// Field is not applicable for DynamicArrayExcludes as it's meant for array structures.
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

// ExcludeEmpty wraps exclusion rules and filters out empty normalized values.
// This is useful for removing empty maps and arrays from the output.
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
	Mapping  ValueMapper  // Function to transform the value
	Continue ExcludeRules // Rules to apply after mapping
}

// MapValue transforms a value using the provided mapping function.
func (m MapValue) MapValue(value interface{}) interface{} {
	if m.Mapping != nil {
		return m.Mapping(value)
	}
	return value
}

// Field applies the mapping and continuation rules to a map field.
func (m MapValue) Field(name string, value interface{}) (string, interface{}, ExcludeRules) {
	if m.Continue != nil {
		return m.Continue.Field(name, value)
	}
	return name, value, NoExcludes{}
}

// Element applies the mapping and continuation rules to an array element.
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
