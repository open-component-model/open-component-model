package spec

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// FlatMap merges the provided configs into a single config.
// The configurations are merged in the order they are provided.
// Nested configurations are flattened in-place into a single configuration.
// Resulting FlatMap is ordered in ascending priority, order of files (and inside of files)
// dictates priority. Later entries override earlier ones.
// Configuration types are decoded the least effort, and if they are not yet decoded,
// they will only be loaded in if they are of type ConfigType.
// All other types will be left as is and taken over.
func FlatMap(configs ...*Config) *Config {
	merged := new(Config)
	merged.Configurations = make([]*runtime.Raw, 0)
	for _, config := range configs {
		for _, entry := range config.Configurations {
			var cfg Config
			if err := Scheme.Convert(entry, &cfg); err != nil {
				merged.Configurations = append(merged.Configurations, entry)
			} else {
				flattened := FlatMap(&cfg)
				if flattened == nil {
					return nil
				}
				merged.Configurations = append(merged.Configurations, flattened.Configurations...)
			}
		}
	}

	return merged
}
