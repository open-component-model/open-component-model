package spec

import (
	"fmt"
	"maps"
	"slices"
)

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
