package v1

import (
	"slices"

	"ocm.software/open-component-model/bindings/go/runtime"
)

type FilterOptions struct {
	ConfigTypes []runtime.Type
}

// Filter filters the config based on the provided options.
// Only the FilterOptions.ConfigTypes are copied over.
// If none are specified, the config will be empty.
func Filter(config *Config, options *FilterOptions) (*Config, error) {
	filtered := new(Config)
	filtered.Type = config.Type

	for _, entry := range config.Configurations {
		configType := entry.GetType()
		if slices.Contains(options.ConfigTypes, configType) {
			filtered.Configurations = append(filtered.Configurations, entry)
		}
	}

	return filtered, nil
}
