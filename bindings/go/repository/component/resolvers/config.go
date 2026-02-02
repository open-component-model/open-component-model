package resolvers

import (
	"fmt"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	resolverruntime "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/runtime"
	resolverv1 "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/spec"
	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// PathMatcherResolversFromConfig extracts path matcher resolvers (v1alpha1) from a generic configuration.
// It filters the configuration for entries of type [resolverspec.Config] and aggregates
// all resolvers defined in these entries into a single list.
func PathMatcherResolversFromConfig(config *genericv1.Config) ([]*resolverspec.Resolver, error) {
	if config == nil || len(config.Configurations) == 0 {
		return nil, nil
	}

	filtered, err := genericv1.FilterForType[*resolverspec.Config](resolverspec.Scheme, config)
	if err != nil {
		return nil, fmt.Errorf("filtering configuration for resolver config failed: %w", err)
	}

	if len(filtered) == 0 {
		return nil, nil
	}

	result := make([]*resolverspec.Resolver, 0, len(filtered))
	for _, r := range filtered {
		result = append(result, r.Resolvers...)
	}

	return result, nil
}

// FallbackResolversFromConfig extracts deprecated fallback resolvers (v1) from a generic configuration.
// It filters the configuration for resolver configurations, merges them, and converts them to runtime format.
//
// Deprecated: Fallback resolvers are deprecated. Use PathMatcherResolversFromConfig instead.
func FallbackResolversFromConfig(config *genericv1.Config, repositoryScheme *runtime.Scheme) ([]*resolverruntime.Resolver, error) {
	if config == nil || len(config.Configurations) == 0 {
		return nil, nil
	}

	filtered, err := genericv1.FilterForType[*resolverv1.Config](resolverv1.Scheme, config)
	if err != nil {
		return nil, fmt.Errorf("filtering configuration for resolver config failed: %w", err)
	}

	if len(filtered) == 0 {
		return nil, nil
	}

	resolverConfigV1 := resolverv1.Merge(filtered...)
	resolverConfig, err := resolverruntime.ConvertFromV1(repositoryScheme, resolverConfigV1)
	if err != nil {
		return nil, fmt.Errorf("converting resolver configuration from v1 to runtime failed: %w", err)
	}

	var resolvers []*resolverruntime.Resolver
	if resolverConfig != nil && len(resolverConfig.Resolvers) > 0 {
		resolvers = make([]*resolverruntime.Resolver, len(resolverConfig.Resolvers))
		for index, resolver := range resolverConfig.Resolvers {
			resolvers[index] = &resolver
		}
	}

	return resolvers, nil
}

// ExtractResolvers extracts both path matcher and fallback resolvers from a generic configuration.
// Returns (fallbackResolvers, pathMatcherResolvers, error).
//
//nolint:staticcheck // compatibility mode for deprecated resolvers
func ExtractResolvers(config *genericv1.Config, repoScheme *runtime.Scheme) ([]*resolverruntime.Resolver, []*resolverspec.Resolver, error) {
	if config == nil {
		return nil, nil, nil
	}

	pathMatcherResolvers, err := PathMatcherResolversFromConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("getting path matchers from configuration failed: %w", err)
	}

	fallbackResolvers, err := FallbackResolversFromConfig(config, repoScheme)
	if err != nil {
		return nil, nil, fmt.Errorf("getting fallback resolvers from configuration failed: %w", err)
	}

	return fallbackResolvers, pathMatcherResolvers, nil
}
