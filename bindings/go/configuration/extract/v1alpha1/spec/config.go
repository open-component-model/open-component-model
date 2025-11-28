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

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{},
		runtime.NewVersionedType(ConfigType, Version),
		runtime.NewUnversionedType(ConfigType),
	)
}

// Config represents the top-level configuration for the transformation.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// +ocm:jsonschema-gen:enum=extract.oci.artifact.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=extract.oci.artifact.ocm.software
	Type runtime.Type `json:"type"`
	// Rules defines rules for extracting layers to specific files.
	Rules []Rule `json:"rules,omitempty"`
}

// Rule represents a rule for extracting selected layers to a target file.
// Multiple layer selectors can be specified to select different layers that should
// be merged into the same output file. For now, the first matching selector is used.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type Rule struct {
	// Filename is the target filename for the extracted layers.
	Filename string `json:"filename,omitempty"`
	// LayerSelectors defines multiple selection criteria for layers to include in this file.
	// Layers matching any of these selectors will be included.
	LayerSelectors []*LayerSelector `json:"layerSelectors,omitempty"`
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
			if err := Scheme.Convert(entry, &config); err != nil {
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
	_, _ = Scheme.DefaultType(merged)

	return merged
}
