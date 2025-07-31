package spec

import (
	"fmt"

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
