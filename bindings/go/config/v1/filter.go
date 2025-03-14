package v1

import (
	"fmt"
	"slices"

	"ocm.software/open-component-model/bindings/go/runtime"
)

type FilterOptions struct {
	RepositoryTypes       []runtime.Type
	ConsumerIdentityTypes []runtime.Type
}

// Filter filters the config based on the provided options.
// Only the FilterOptions.RepositoryTypes and FilterOptions.ConsumerIdentityTypes are copied over.
// For repositories, only those with a type in FilterOptions.RepositoryTypes are copied over.
// For consumers, only those with an identity type in FilterOptions.ConsumerIdentityTypes are copied over, and only
// the identity types are filtered.
// If none are specified, the config will be empty.
func Filter(config *Config, options *FilterOptions) (*Config, error) {
	filtered := new(Config)
	filtered.Type = config.Type
	filtered.Repositories = make([]RepositoryConfigEntry, 0)
	filtered.Consumers = make([]Consumer, 0)

	for _, entry := range config.Repositories {
		repositoryType := entry.Repository.GetType()
		if slices.Contains(options.RepositoryTypes, repositoryType) {
			filtered.Repositories = append(filtered.Repositories, entry)
		}
	}
	for _, consumer := range config.Consumers {
		matchedIdentities := make([]Identity, 0)
		for _, identity := range consumer.Identities {
			consumerIdentityType, ok := identity[IdentityAttributeType]
			if !ok {
				return nil, fmt.Errorf("failed to parse consumer identity attribute %q", IdentityAttributeType)
			}
			if slices.Contains(options.ConsumerIdentityTypes, runtime.NewUngroupedUnversionedType(consumerIdentityType)) {
				matchedIdentities = append(matchedIdentities, identity)
			}
		}
		if len(matchedIdentities) > 0 {
			filtered.Consumers = append(filtered.Consumers, Consumer{
				Identities:  slices.Clone(matchedIdentities),
				Credentials: slices.Clone(consumer.Credentials),
			})
		}
	}

	return filtered, nil
}
