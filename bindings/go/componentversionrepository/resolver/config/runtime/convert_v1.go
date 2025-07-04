package runtime

import (
	"fmt"
	"log/slog"

	resolverv1 "ocm.software/open-component-model/bindings/go/componentversionrepository/resolver/config/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

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
		converted[i] = Resolver{
			Repository: convertedRepo,
			Prefix:     resolver.Prefix,
			Priority:   resolver.Priority,
		}
	}
	return converted, nil
}
