package runtime

import (
	"fmt"
	"log/slog"

	resolverv1 "ocm.software/open-component-model/bindings/go/repositories/componentrepository/resolver/config/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ConvertFromV1 converts a resolver configuration from the v1 version to the runtime.Config type.
//
// Deprecated: Resolvers are deprecated and are only added for backwards
// compatibility.
// New concepts will likely be introduced in the future (contributions welcome!).
// nolint:staticcheck // SA1019: using deprecated type within deprecated code
func ConvertFromV1(repositoryScheme *runtime.Scheme, config *resolverv1.Config) (*Config, error) {
	if config == nil {
		return nil, nil
	}
	if len(config.Aliases) > 0 {
		slog.Info("aliases are not supported in ocm v2, ignoring")
	}
	convertedResolvers, err := convertResolvers(repositoryScheme, config.Resolvers)
	if err != nil {
		return nil, fmt.Errorf("failed to convert resolvers to their runtime type: %w", err)
	}

	return &Config{
		Type:      config.Type,
		Resolvers: convertedResolvers,
	}, nil
}

// Deprecated
// nolint:staticcheck // SA1019: using deprecated type within deprecated code
func convertResolvers(repositoryScheme *runtime.Scheme, resolvers []*resolverv1.Resolver) ([]Resolver, error) {
	if len(resolvers) == 0 {
		return nil, nil
	}

	converted := make([]Resolver, len(resolvers))
	for i, resolver := range resolvers {
		convertedRepo, err := repositoryScheme.NewObject(resolver.Repository.GetType())
		if err != nil {
			return nil, err
		}
		if err := repositoryScheme.Convert(resolver.Repository, convertedRepo); err != nil {
			return nil, err
		}

		var priority int
		if resolver.Priority != nil {
			priority = *resolver.Priority
		} else {
			priority = resolverv1.DefaultLookupPriority
		}

		converted[i] = Resolver{
			Repository: convertedRepo,
			Prefix:     resolver.Prefix,
			Priority:   priority,
		}
	}
	return converted, nil
}
