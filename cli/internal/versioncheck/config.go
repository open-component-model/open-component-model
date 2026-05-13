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

	// PolicyAuto enables automatic version checking (default behavior).
	PolicyAuto = "auto"
	// PolicyDisable suppresses all version check activity (no network calls, no warnings).
	PolicyDisable = "disable"
)

var configScheme = runtime.NewScheme()

func init() {
	configScheme.MustRegisterWithAlias(&Config{}, runtime.NewVersionedType(ConfigType, ConfigVersion))
}

// Config configures the default policy for version checking.
// The policy field controls whether the CLI performs automatic update checks.
// Set to "disable" to suppress all version check activity for an environment.
//
// Example config:
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	- type: versioncheck.cli.config.ocm.software/v1alpha1
//	  policy: disable
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// +ocm:jsonschema-gen:enum=versioncheck.cli.config.ocm.software/v1alpha1
	Type runtime.Type `json:"type"`
	// Policy controls version check behavior. Valid values are "auto" (default) and "disable".
	// +ocm:jsonschema-gen:enum=auto,disable
	Policy string `json:"policy"`
}

// LookupConfig extracts the version check configuration from a generic OCM config.
// Returns a default (policy: auto) Config if no matching configuration entry is found.
func LookupConfig(cfg *generic.Config) (*Config, error) {
	if cfg == nil {
		return &Config{Policy: PolicyAuto}, nil
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
		if config.Policy != "" {
			if config.Policy != PolicyAuto && config.Policy != PolicyDisable {
				return nil, fmt.Errorf("invalid versioncheck policy %q: must be %q or %q", config.Policy, PolicyAuto, PolicyDisable)
			}
			return &config, nil
		}
	}

	return &Config{Policy: PolicyAuto}, nil
}
