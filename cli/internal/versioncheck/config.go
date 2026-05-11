package versioncheck

import (
	"fmt"

	generic "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	ConfigType    = "versioncheck.cli.config.ocm.software"
	ConfigVersion = "v1alpha1"
)

var configScheme = runtime.NewScheme()

func init() {
	// configScheme.MustRegisterWithAlias(&Config{}, runtime.NewVersionedType(ConfigType, ConfigVersion))
}

// Config configures behavior for version checking
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// +ocm:jsonschema-gen:enum=versioncheck.cli.config.ocm.software/v1alpha1
	Type     runtime.Type `json:"type"`
	Disabled bool         `json:"disabled"`
}

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
