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

// Layer has configuration information for layers to process.
type Layer struct {
	Index     int    `json:"index,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
}

// Config represents the top-level configuration for the transformation.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type Config struct {
	Type runtime.Type `json:"type"`
	// Layer defines configuration options on selecting the right Layer.
	Layer Layer `json:"layer,omitempty"`
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
