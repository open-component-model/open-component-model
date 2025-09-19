package ocm

import (
	"fmt"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	resolverruntime "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/runtime"
	resolverv1 "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/spec"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/spec/repository"
)

//nolint:staticcheck // no replacement for resolvers available yet https://github.com/open-component-model/ocm-project/issues/575
func ResolversFromConfig(config *genericv1.Config) ([]*resolverruntime.Resolver, error) {
	filtered, err := genericv1.FilterForType[*resolverv1.Config](resolverv1.Scheme, config)
	if err != nil {
		return nil, fmt.Errorf("filtering configuration for resolver config failed: %w", err)
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
