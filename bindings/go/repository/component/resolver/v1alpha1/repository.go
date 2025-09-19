package v1alpha1

import (
	"context"
	"errors"
	"fmt"

	"ocm.software/open-component-model/bindings/go/configuration/resolvers/v1/matcher"
	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	Realm       = "repository/component/resolver"
	IdentityKey = "componentName"
)

// SpecResolver implements a RepositorySpecProvider with a resolver
// mechanism. It uses glob patterns to match component names to
// determine which OCM repository specification to use for resolving
// component versions.
type SpecResolver struct {
	// A list of resolvers to use for matching components to repositories.
	// This list is immutable after creation.
	resolvers []*resolverspec.Resolver

	// This cache is based on index. So, the index of the resolver in the
	// resolver slice corresponds to the index of the repository in this slice.
	matchers []*matcher.ResolverMatcher
}

// NewSpecResolver creates a new SpecResolver with a list of resolvers.
// The resolvers are used to match component names to repository specifications.
func NewSpecResolver(_ context.Context, res []*resolverspec.Resolver) (*SpecResolver, error) {
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

	return &SpecResolver{
		resolvers: resolvers,
		matchers:  matchers,
	}, nil
}

// GetRepositorySpec returns the repository specification for the given component identity.
// It matches the component name against the configured resolvers and returns
// the first matching repository specification.
// If no matching resolver is found, an error is returned.
// componentIdentity must contain the key [IdentityKey] containing the name of the component e.g. "ocm.software/core/test".
func (r *SpecResolver) GetRepositorySpec(_ context.Context, componentIdentity runtime.Identity) (runtime.Typed, error) {
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
