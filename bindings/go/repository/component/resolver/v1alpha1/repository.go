package v1alpha1

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"ocm.software/open-component-model/bindings/go/configuration/resolvers/v1/matcher"
	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const Realm = "repository/component/resolver"
const IdentityKey = "componentName"

// ResolverRepositorySpecProvider implements a component version repository with a resolver
// mechanism. It uses glob patterns to match component names to
// determine which OCM repository specification to use for resolving
// component versions.
type ResolverRepositorySpecProvider struct {

	// A list of resolvers to use for matching components to repositories.
	// This list is immutable after creation.
	resolvers []*resolverspec.Resolver

	// This cache is based on index. So, the index of the resolver in the
	// resolver slice corresponds to the index of the repository in this slice.
	matchersMu sync.RWMutex
	matchers   []*matcher.ResolverMatcher
}

// NewResolverRepository creates a new ResolverRepositorySpecProvider with the given
// repository provider, credential provider, and list of resolvers.
// The repository provider is used to create repositories based on the
// repository specifications in the resolvers.
// The credential provider is used to resolve credentials for the repositories.
func NewResolverRepository(_ context.Context, res []*resolverspec.Resolver) (*ResolverRepositorySpecProvider, error) {
	resolvers := deepCopyResolvers(res)

	var matchers []*matcher.ResolverMatcher
	var resolverErrs []error
	for i, resolver := range resolvers {
		m, err := matcher.NewResolverMatcher(resolver.ComponentName)
		if err != nil {
			resolverErrs = append(resolverErrs, fmt.Errorf("failed to create matcher for resolver %d: %w", i, err))
			continue
		}
		matchers = append(matchers, m)
	}

	if len(resolverErrs) > 0 {
		return nil, fmt.Errorf("one or more resolvers are invalid: %w", errors.Join(resolverErrs...))
	}

	return &ResolverRepositorySpecProvider{
		resolvers: resolvers,
		matchers:  matchers,
	}, nil
}

func (r *ResolverRepositorySpecProvider) GetRepositorySpec(_ context.Context, componentIdentity runtime.Identity) (runtime.Typed, error) {
	componentName, ok := componentIdentity[IdentityKey]
	if !ok || componentName == "" {
		return nil, fmt.Errorf("failed to extract component name from identity %s", componentIdentity)
	}

	for index, resolver := range r.resolvers {
		if r.matchers[index].Match(componentName, "") {
			return resolver.Repository, nil
		}
	}

	return nil, fmt.Errorf("no repository found for component identity %s", componentIdentity)
}

func deepCopyResolvers(resolvers []*resolverspec.Resolver) []*resolverspec.Resolver {
	if resolvers == nil {
		return nil
	}
	copied := make([]*resolverspec.Resolver, 0, len(resolvers))
	for _, resolver := range resolvers {
		copied = append(copied, resolver.DeepCopy())
	}
	return copied
}
