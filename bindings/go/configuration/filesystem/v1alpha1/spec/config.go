package spec

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// ConfigType defines the type identifier for credential configurations
	ConfigType = "filesystem.config.ocm.software"
)

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{}, runtime.NewVersionedType(ConfigType, Version))
}

// Config represents the top-level configuration for the plugin manager.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// +ocm:jsonschema-gen:enum=filesystem.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=filesystem.config.ocm.software
	Type runtime.Type `json:"type"`

	// TempFolder defines places where plugins and other functionalities can put ephemeral files under.
	// If not defined, os.TempDir is used as a default.
	TempFolder string `json:"tempFolder,omitempty"`

	// WorkingDirectory defines the working directory for the filesystem operations.
	// This is typically the directory where the plugin operates, and it can be used
	// to resolve relative paths for file operations.
	// If not defined, the current working directory is used as a default for file operations.
	WorkingDirectory string `json:"workingDirectory,omitempty"`
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

	if len(merged.TempFolder) == 0 {
		merged.TempFolder = os.TempDir()
	}

	return merged, nil
}

// Merge merges the provided configs into a single config.
func Merge(configs ...*Config) *Config {
	if len(configs) == 0 {
		return nil
	}

	merged := new(Config)
	_, _ = Scheme.DefaultType(merged)

	for _, config := range configs {
		if config.TempFolder != merged.TempFolder {
			merged.TempFolder = config.TempFolder
		}
		if config.WorkingDirectory != merged.WorkingDirectory {
			merged.WorkingDirectory = config.WorkingDirectory
		}
	}

	return merged
}
