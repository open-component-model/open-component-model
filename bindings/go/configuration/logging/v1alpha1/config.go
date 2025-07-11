package v1alpha1

import (
	"encoding/json"
	"fmt"
	"time"

	v1 "ocm.software/open-component-model/bindings/go/configuration/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// ConfigType defines the type identifier for logging configurations
	ConfigType = "logging.config.ocm.software"
)

var scheme = runtime.NewScheme()

func init() {
	scheme.MustRegisterWithAlias(&Config{}, runtime.NewVersionedType(ConfigType, Version))
}

// Config represents the top-level configuration for logging.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type Config struct {
	Type runtime.Type `json:"-"`
	// LoggingLevel defines the level.
	LoggingLevel string `json:"loggingLevel,omitempty"`
	// LoggingFormat defines logging format.
	LoggingFormat string `json:"loggingFormat,omitempty"`
}

type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var v interface{}

	return json.Unmarshal(b, &v)
}

// LookupConfig creates a new filesystem configuration from a central V1 config.
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

	if len(merged.LoggingLevel) == 0 {
		merged.LoggingLevel = "info"
	}
	if len(merged.LoggingFormat) == 0 {
		merged.LoggingFormat = "json"
	}

	return merged, nil
}

// Merge merges the provided configs into a single config.
func Merge(configs ...*Config) *Config {
	if len(configs) == 0 {
		return nil
	}

	merged := new(Config)
	_, _ = scheme.DefaultType(merged)

	for _, config := range configs {
		if config.LoggingLevel != merged.LoggingLevel {
			merged.LoggingLevel = config.LoggingLevel
		}
		if config.LoggingFormat != merged.LoggingFormat {
			merged.LoggingFormat = config.LoggingFormat
		}
	}

	return merged
}
