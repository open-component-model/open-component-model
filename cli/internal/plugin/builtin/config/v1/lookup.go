package v1

import (
	"bytes"
	"fmt"

	configv1 "ocm.software/open-component-model/bindings/go/configuration/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&BuiltinPluginConfig{}, runtime.NewVersionedType(ConfigType, ConfigTypeV1))
	Scheme.MustRegisterWithAlias(&BuiltinPluginConfig{}, runtime.NewUnversionedType(ConfigType))
}

// LookupBuiltinPluginConfig extracts the builtin plugin configuration from the global configuration.
func LookupBuiltinPluginConfig(config *configv1.Config) (*BuiltinPluginConfig, error) {
	if config == nil {
		return DefaultBuiltinPluginConfig(), nil
	}

	filtered, err := configv1.Filter(config, &configv1.FilterOptions{
		ConfigTypes: []runtime.Type{
			runtime.NewVersionedType(ConfigType, ConfigTypeV1),
			runtime.NewUnversionedType(ConfigType),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to filter builtin plugin configuration: %w", err)
	}

	// Start with defaults
	builtinConfig := DefaultBuiltinPluginConfig()
	if len(filtered.Configurations) == 0 {
		return builtinConfig, nil
	}

	// TODO: Figure out what to do about multiple configurations. For now, use the latest.
	raw := filtered.Configurations[len(filtered.Configurations)-1]

	var tempConfig BuiltinPluginConfig
	if err := Scheme.Decode(bytes.NewReader(raw.Data), &tempConfig); err != nil {
		return nil, fmt.Errorf("failed to decode builtin plugin configuration: %w", err)
	}

	// Override defaults with non-zero values from config
	if tempConfig.LogLevel != "" {
		builtinConfig.LogLevel = tempConfig.LogLevel
	}
	if tempConfig.LogFormat != "" {
		builtinConfig.LogFormat = tempConfig.LogFormat
	}
	if tempConfig.TempFolder != "" {
		builtinConfig.TempFolder = tempConfig.TempFolder
	}

	return builtinConfig, nil
}
