package spec

import (
	"fmt"
	"slices"

	v1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// ConfigType defines the type identifier for transformation configurations
	ConfigType = "extract.oci.artifact.ocm.software"
)

var scheme = runtime.NewScheme()

func init() {
	scheme.MustRegisterWithAlias(&Config{}, runtime.NewVersionedType(ConfigType, Version))
}

// Predefined selector keys for layer properties
const (
	// LayerIndexKey is the key used to select layers by index
	LayerIndexKey = "layer.index"
	// LayerMediaTypeKey is the key used to select layers by media type
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
type LayerSelector struct {
	// MatchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
	// map is equivalent to an element of matchExpressions, whose key field is "key", the
	// operator is "In", and the value array contains only "value". *Note* this is called
	// match_Labels_ to be in-line with other, more common, matching selector operations.
	// Though technically, no labels are matched.
	MatchLabels map[string]string `json:"matchLabels,omitempty"`
	// MatchExpressions is a list of selectors. The selectors are ANDed together.
	// Use predefined keys like 'layer.index' and 'layer.mediaType' for built-in properties.
	MatchExpressions []LayerSelectorRequirement `json:"matchExpressions,omitempty"`
}

// Config represents the top-level configuration for the transformation.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type Config struct {
	Type runtime.Type `json:"type"`
	// LayerSelector defines a selection criteria for layers.
	LayerSelector *LayerSelector `json:"layerSelector,omitempty"`
}

// LookupConfig creates a new extract configuration from a central V1 config.
func LookupConfig(cfg *v1.Config) (*Config, error) {
	var merged *Config
	if cfg != nil {
		cfg, err := v1.Filter(cfg, &v1.FilterOptions{
			ConfigTypes: []runtime.Type{
				runtime.NewVersionedType(ConfigType, Version),
				runtime.NewUnversionedType(ConfigType),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to filter config: %w", err)
		}
		cfgs := make([]*Config, 0, len(cfg.Configurations))
		for _, entry := range cfg.Configurations {
			var config Config
			if err := scheme.Convert(entry, &config); err != nil {
				return nil, fmt.Errorf("failed to decode credential config: %w", err)
			}
			cfgs = append(cfgs, &config)
		}
		merged = Merge(cfgs...)
		if merged == nil {
			merged = &Config{}
		}
	} else {
		merged = new(Config)
	}

	// Update later with values to configure.

	return merged, nil
}

// Merge merges the provided configs into a single config.
func Merge(configs ...*Config) *Config {
	if len(configs) == 0 {
		return nil
	}

	merged := new(Config)
	_, _ = scheme.DefaultType(merged)

	return merged
}

// LayerInfo represents information about a layer for matching purposes.
// The user populates this layer info to call Matches on the selectors.
type LayerInfo struct {
	Index     int
	MediaType string
	// Potentially add annotations to these properties.
}

// GetProperties returns a combined map of all layer properties for matching.
// Includes predefined properties `index` and `mediaType`.
func (li LayerInfo) GetProperties() map[string]string {
	props := make(map[string]string)

	// Add predefined properties
	props[LayerIndexKey] = fmt.Sprintf("%d", li.Index)
	props[LayerMediaTypeKey] = li.MediaType

	// TODO: Merge annotations

	return props
}

// Matches returns true if the layer selector matches the given layer info.
func (ls *LayerSelector) Matches(layer LayerInfo) bool {
	if ls == nil {
		return true // nil selector matches everything
	}

	props := layer.GetProperties()

	// Check match labels
	if !ls.matchesLabels(props) {
		return false
	}

	// Check match expressions
	return ls.matchesExpressions(props)
}

// matchesLabels checks if all match labels are satisfied.
func (ls *LayerSelector) matchesLabels(properties map[string]string) bool {
	if len(ls.MatchLabels) == 0 {
		return true
	}

	for key, expectedValue := range ls.MatchLabels {
		actualValue, exists := properties[key]
		if !exists || actualValue != expectedValue {
			return false
		}
	}
	return true
}

// matchesExpressions checks if all match expressions are satisfied.
func (ls *LayerSelector) matchesExpressions(properties map[string]string) bool {
	for _, expr := range ls.MatchExpressions {
		if !ls.matchesExpression(expr, properties) {
			return false
		}
	}
	return true
}

// matchesExpression checks if a single expression is satisfied.
func (ls *LayerSelector) matchesExpression(expr LayerSelectorRequirement, properties map[string]string) bool {
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
