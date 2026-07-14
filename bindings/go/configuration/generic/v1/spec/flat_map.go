package spec

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// FlatMap merges the provided configs into a single config.
// The configurations are merged in the order they are provided.
// Nested configurations are flattened into a single configuration.
// Resulting FlatMap is ordered in ascending priority, later entries override earlier ones.
// Order of files (and within files) dictates priority.
//
// Exception: for compatibility with v1 config formats, nested configs take precedence
// over direct sibling configs. Direct entries are collected first, then nested generic
// children are appended after them. For example:
//
//	configurations:
//	  - type: generic
//	    configurations:
//	      - type: transfer
//	        copyMode: localBlob
//	  - type: transfer
//	    copyMode: allResources
//
// FlatMap produces: [transfer(allResources), transfer(localBlob)].
//
// Configuration types are decoded the least effort, and if they are not yet decoded,
// they will only be loaded in if they are of type ConfigType.
// All other types will be left as is and taken over.
func FlatMap(configs ...*Config) *Config {
	merged := new(Config)
	merged.Configurations = make([]*runtime.Raw, 0)
	for _, config := range configs {
		flattenCandidates := make([]*Config, 0)
		for _, entry := range config.Configurations {
			var cfg Config
			if err := Scheme.Convert(entry, &cfg); err != nil {
				merged.Configurations = append(merged.Configurations, entry)
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

	return merged
}
