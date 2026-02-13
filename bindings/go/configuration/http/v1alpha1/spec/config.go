package spec

import (
	"encoding/json"
	"fmt"
	"time"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// ConfigType defines the type identifier for HTTP client configurations.
	ConfigType = "http.config.ocm.software"
)

// DefaultTimeout is the default HTTP client timeout used when no
// configuration is provided.
const DefaultTimeout = Timeout(30 * time.Second)

// Timeout wraps time.Duration to support JSON/YAML marshaling
// of human-readable duration strings (e.g. "30s", "5m", "1h").
type Timeout time.Duration

func (d Timeout) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Timeout) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return fmt.Errorf("failed to parse HTTP client timeout: %w", err)
	}

	switch value := v.(type) {
	case float64:
		*d = Timeout(time.Duration(value))
		return nil
	case string:
		tmp, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("invalid timeout value %q: must be a duration like 30s, 5m, or nanoseconds number: %w", value, err)
		}
		*d = Timeout(tmp)
		return nil
	default:
		return fmt.Errorf("timeout must be a duration string or nanoseconds number, got %T", v)
	}
}

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{},
		runtime.NewVersionedType(ConfigType, Version),
		runtime.NewUnversionedType(ConfigType),
	)
}

// Config represents the HTTP client configuration.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// +ocm:jsonschema-gen:enum=http.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=http.config.ocm.software
	Type runtime.Type `json:"type"`

	// Timeout specifies the HTTP client timeout as a Go duration string
	// (e.g. "30s", "5m", "1h"). If not set, the default timeout of 30s is used.
	Timeout Timeout `json:"timeout,omitempty"`
}

// LookupConfig creates an HTTP configuration from a central generic V1 config.
func LookupConfig(cfg *genericv1.Config) (*Config, error) {
	var merged *Config
	if cfg != nil {
		cfg, err := genericv1.Filter(cfg, &genericv1.FilterOptions{
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
				return nil, fmt.Errorf("failed to decode http config: %w", err)
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

	if merged.Timeout == 0 {
		merged.Timeout = DefaultTimeout
	}

	return merged, nil
}

// Merge merges the provided configs into a single config.
// The last non-zero timeout wins.
func Merge(configs ...*Config) *Config {
	if len(configs) == 0 {
		return nil
	}

	merged := new(Config)
	_, _ = Scheme.DefaultType(merged)

	for _, config := range configs {
		if config.Timeout != 0 {
			merged.Timeout = config.Timeout
		}
	}

	return merged
}
