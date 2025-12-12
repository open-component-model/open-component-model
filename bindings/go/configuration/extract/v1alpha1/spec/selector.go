package spec

import (
	"fmt"
	"maps"
	"slices"
)

// Predefined selector keys for layer properties.
const (
	// LayerIndexKey is the key used to select layers by index.
	LayerIndexKey = "layer.index"
	// LayerMediaTypeKey is the key used to select layers by media type.
	LayerMediaTypeKey = "layer.mediaType"
)

// LayerSelectorOperator represents the operator for selection expressions.
type LayerSelectorOperator string

const (
	// LayerSelectorOpIn - the value is in the set of values
	LayerSelectorOpIn LayerSelectorOperator = "In"
	// LayerSelectorOpNotIn - the value is not in the set of values
	LayerSelectorOpNotIn LayerSelectorOperator = "NotIn"
	// LayerSelectorOpExists - the key exists
	LayerSelectorOpExists LayerSelectorOperator = "Exists"
	// LayerSelectorOpDoesNotExist - the key does not exist
	LayerSelectorOpDoesNotExist LayerSelectorOperator = "DoesNotExist"
)

// LayerSelectorRequirement represents a single requirement for layer selection.
// +k8s:deepcopy-gen=true
type LayerSelectorRequirement struct {
	// Key is the property key that the selector applies to.
	// Can be a custom annotation key or predefined keys like layer.index, layer.mediaType.
	Key string `json:"key"`
	// Operator represents the relationship between the key and values.
	Operator LayerSelectorOperator `json:"operator"`
	// Values is an array of string values. If the operator is In or NotIn,
	// the value array must be non-empty. If the operator is Exists or DoesNotExist,
	// the value array must be empty.
	Values []string `json:"values,omitempty"`
}

// LayerSelector allows selecting layers based on index, mediatype, and annotations.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type LayerSelector struct {
	// MatchProperties is a map of {key,value} pairs. A single {key,value} in the matchLabels
	// map is equivalent to an element of matchExpressions, whose key field is "key", the
	// operator is "In", and the value array contains only "value".
	MatchProperties map[string]string `json:"matchProperties,omitempty"`
	// MatchExpressions is a list of selectors. The selectors are ANDed together.
	// Use predefined keys like 'layer.index' and 'layer.mediaType' for built-in properties.
	MatchExpressions []LayerSelectorRequirement `json:"matchExpressions,omitempty"`
}

// LayerInfo represents information about a layer for matching purposes.
// The user populates this layer info to call Matches on the selectors.
type LayerInfo struct {
	Index       int
	MediaType   string
	Annotations map[string]string
}

// GetProperties returns a combined map of all layer properties for matching.
// Includes predefined properties `index` and `mediaType`.
func (l LayerInfo) GetProperties() map[string]string {
	props := make(map[string]string)

	// Add predefined properties
	props[LayerIndexKey] = fmt.Sprintf("%d", l.Index)
	props[LayerMediaTypeKey] = l.MediaType
	maps.Copy(props, l.Annotations)

	return props
}

// Matches returns true if the layer selector matches the given layer info.
func (l *LayerSelector) Matches(layer LayerInfo) bool {
	if l == nil {
		return true // nil selector matches everything
	}

	props := layer.GetProperties()

	// Check match properties
	if !l.matchesProperties(props) {
		return false
	}

	// Check match expressions
	return l.matchesExpressions(props)
}

// matchesProperties checks if all match Properties are satisfied.
func (l *LayerSelector) matchesProperties(properties map[string]string) bool {
	if len(l.MatchProperties) == 0 {
		return true
	}

	for key, expectedValue := range l.MatchProperties {
		actualValue, exists := properties[key]
		if !exists || actualValue != expectedValue {
			return false
		}
	}
	return true
}

// matchesExpressions checks if all match expressions are satisfied.
func (l *LayerSelector) matchesExpressions(properties map[string]string) bool {
	for _, expr := range l.MatchExpressions {
		if !l.matchesExpression(expr, properties) {
			return false
		}
	}
	return true
}

// matchesExpression checks if a single expression is satisfied.
func (l *LayerSelector) matchesExpression(expr LayerSelectorRequirement, properties map[string]string) bool {
	actualValue, exists := properties[expr.Key]
	switch expr.Operator {
	case LayerSelectorOpExists:
		return exists
	case LayerSelectorOpDoesNotExist:
		return !exists
	case LayerSelectorOpIn:
		if !exists {
			return false
		}
		return slices.Contains(expr.Values, actualValue)
	case LayerSelectorOpNotIn:
		if !exists {
			return true
		}
		return !slices.Contains(expr.Values, actualValue)
	default:
		return false
	}
}
