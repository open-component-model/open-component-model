package versioncheck

import (
	"fmt"

	generic "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// ConfigType is the OCM configuration type identifier for version check settings.
	ConfigType = "versioncheck.cli.config.ocm.software"
	// ConfigVersion is the schema version for the version check configuration.
	ConfigVersion = "v1alpha1"
)

var configScheme = runtime.NewScheme()

func init() {
	configScheme.MustRegisterWithAlias(&Config{}, runtime.NewVersionedType(ConfigType, ConfigVersion))
}

// Config configures behavior for version checking.
// Users can place this in their OCM config file to persistently disable the check:
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	- type: versioncheck.cli.config.ocm.software/v1alpha1
//	  disabled: true
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// +ocm:jsonschema-gen:enum=versioncheck.cli.config.ocm.software/v1alpha1
	Type runtime.Type `json:"type"`
	// Disabled, when true, suppresses all version check activity (no network calls, no warnings).
	Disabled bool `json:"disabled"`
}

// LookupConfig extracts the version check configuration from a generic OCM config.
// Returns a default (enabled) Config if no matching configuration entry is found.
func LookupConfig(cfg *generic.Config) (*Config, error) {
	if cfg == nil {
		return &Config{}, nil
	}

	filtered, err := generic.Filter(cfg, &generic.FilterOptions{
		ConfigTypes: []runtime.Type{runtime.NewVersionedType(ConfigType, ConfigVersion)},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to filter versioncheck config: %w", err)
	}

	for _, entry := range filtered.Configurations {
		var config Config
		if err := configScheme.Convert(entry, &config); err != nil {
			return nil, fmt.Errorf("failed to decode versioncheck config: %w", err)
		}
		if config.Disabled {
			return &config, nil
		}
	}

	return &Config{}, nil
}
