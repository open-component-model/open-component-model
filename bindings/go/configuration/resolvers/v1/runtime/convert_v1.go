package runtime

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/configuration/resolvers/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func ConvertFromV1(scheme *runtime.Scheme, specConfig *spec.Config) (*Config, error) {
	if specConfig == nil {
		return nil, nil
	}

	runtimeConfig := &Config{
		Type:      specConfig.Type,
		Resolvers: make([]Resolver, 0, len(specConfig.Resolvers)),
	}

	for _, specResolver := range specConfig.Resolvers {
		if specResolver == nil {
			continue
		}

		var repository runtime.Typed
		if specResolver.Repository != nil {
			repository = specResolver.Repository.DeepCopy()
		}

		runtimeResolver := Resolver{
			Repository:    repository,
			ComponentName: specResolver.ComponentName,
			SemVer:        specResolver.SemVer,
		}

		runtimeConfig.Resolvers = append(runtimeConfig.Resolvers, runtimeResolver)
	}

	return runtimeConfig, nil
}

func ConvertToV1(scheme *runtime.Scheme, runtimeConfig *Config) (*spec.Config, error) {
	if runtimeConfig == nil {
		return nil, nil
	}

	specConfig := &spec.Config{
		Type:      runtimeConfig.Type,
		Resolvers: make([]*spec.Resolver, 0, len(runtimeConfig.Resolvers)),
	}

	for _, runtimeResolver := range runtimeConfig.Resolvers {
		var repository *runtime.Raw
		if runtimeResolver.Repository != nil {
			repository = &runtime.Raw{}
			if err := scheme.Convert(runtimeResolver.Repository, repository); err != nil {
				return nil, fmt.Errorf("failed to convert repository specification to raw: %w", err)
			}
		}

		specResolver := &spec.Resolver{
			Repository:    repository,
			ComponentName: runtimeResolver.ComponentName,
			SemVer:        runtimeResolver.SemVer,
		}

		specConfig.Resolvers = append(specConfig.Resolvers, specResolver)
	}

	return specConfig, nil
}
