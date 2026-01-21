package setup

import (
	"fmt"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	resolverruntime "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/runtime"
	resolverv1 "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/spec"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/spec/repository"
)

// GetResolvers extracts resolver configuration from the generic config.
// Resolvers specify fallback repositories for component references.
//
// Deprecated: This function extracts deprecated fallback resolvers. Use GetResolversV1Alpha1
// to get the modern path matcher resolvers instead. Fallback resolvers are kept for backward
// compatibility but will be removed in a future version.
//
// TODO: Question. This is now unused because I don't support the old repo matching, should we or
// should we say the new controllers only follow the new resolvers?
func GetResolvers(config *genericv1.Config) ([]*resolverruntime.Resolver, error) {
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
	resolverConfig, err := resolverruntime.ConvertFromV1(ocirepository.Scheme, resolverConfigV1)
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
