package runtime

// Merge merges the provided configs into a single config.
// If the configs have multiple repositories with the same priority, the order
// of configs is relevant (first will be preferred).
func Merge(configs ...*Config) *Config {
	if len(configs) == 0 {
		return nil
	}

	merged := new(Config)
	merged.Type = configs[0].Type
	merged.Resolvers = make([]Resolver, 0)

	for _, config := range configs {
		merged.Resolvers = append(merged.Resolvers, config.Resolvers...)
	}

	return merged
}
