package v1

import (
	"slices"
)

// FlatMap merges the provided configs into a single config.
// The configurations are merged in the order they are provided.
// Nested configurations are flattened into a single configuration.
// Configuration types are decoded the least effort, and if they are not yet decoded,
// they will only be loaded in if they are of type ConfigType.
// All other types will be left as is and taken over.
func FlatMap(configs ...*Config) (*Config, error) {
	merged := new(Config)
	merged.Configurations = make([]Configuration, 0)
	for _, config := range configs {
		flattenCandidates := make([]*Config, 0)
		for _, config := range config.Configurations {
			var cfg Config
			if err := scheme.Convert(config.Raw, &cfg); err != nil {
				merged.Configurations = append(merged.Configurations, config)
			} else {
				flattenCandidates = append(flattenCandidates, &cfg)
			}
		}

		cfg, err := FlatMap(flattenCandidates...)
		if err != nil {
			return nil, err
		}

		merged.Configurations = append(merged.Configurations, cfg.Configurations...)
	}
	// reverse the order of the configurations to match the order of the input configs
	// this is important for the order of the configurations to be preserved.
	slices.Reverse(merged.Configurations)
	return merged, nil
}
