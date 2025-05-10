package v1

import (
	"slices"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// FlatMap merges the provided configs into a single config.
// The configurations are merged in the order they are provided.
// Nested configurations are flattened into a single configuration.
// Configuration types are decoded the least effort, and if they are not yet decoded,
// they will only be loaded in if they are of type ConfigType.
// All other types will be left as is and taken over.
func FlatMap(configs ...*Config) *Config {
	merged := new(Config)
	merged.Configurations = make([]*runtime.Raw, 0)
	for _, config := range configs {
		flattenCandidates := make([]*Config, 0)
		for _, config := range config.Configurations {
			var cfg Config
			if err := Scheme.Convert(config, &cfg); err != nil {
				merged.Configurations = append(merged.Configurations, config)
			} else {
				flattenCandidates = append(flattenCandidates, &cfg)
			}
		}

		cfg := FlatMap(flattenCandidates...)
		if cfg == nil {
			return nil
		}

		merged.Configurations = append(merged.Configurations, cfg.Configurations...)
	}

	// reverse the order of the configurations to match the order of the input configs
	// this is important for the order of the configurations to be preserved.
	// In case of Configuration Sets declared in a Config file the LAST item in the list
	// should be the one overwriting any preceding items.
	slices.Reverse(merged.Configurations)
	return merged
}
