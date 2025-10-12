package v1alpha1

import (
	"context"
	"fmt"

	"github.com/gobwas/glob"

	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1alpha1/spec"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// SpecProvider implements a ComponentVersionRepositorySpecProvider with
// a resolver mechanism. It uses path patterns leveraging the github.com/gobwas/glob
// library to match component names to determine which OCM repository
// specification to use for resolving component versions.
type SpecProvider struct {
	// A list of resolvers to use for matching components to repositories.
	// This list is immutable after creation.
	resolvers []*resolverspec.Resolver
}

// NewSpecProvider creates a new SpecProvider with a list of resolvers.
// The resolvers are used to match component names to repository specifications.
func NewSpecProvider(_ context.Context, resolvers []*resolverspec.Resolver) *SpecProvider {
	return &SpecProvider{
		resolvers: resolvers,
	}
}

// GetRepositorySpec returns the repository specification for the given component identity.
// It matches the component name against the configured resolvers and returns
// the first matching repository specification.
// If no matching resolver is found, an error is returned.
// componentIdentity must contain the key [IdentityKey] containing the name of the component e.g. "ocm.software/core/test".
func (r *SpecProvider) GetRepositorySpec(_ context.Context, componentIdentity runtime.Identity) (runtime.Typed, error) {
	componentName, ok := componentIdentity[descruntime.IdentityAttributeName]
	if !ok {
		return nil, fmt.Errorf("failed to extract component name from identity %s", componentIdentity)
	}

	for index, resolver := range r.resolvers {
		g, err := glob.Compile(resolver.ComponentNamePattern)
		if err != nil {
			return nil, fmt.Errorf("failed to compile glob pattern %q in resolver index %d: %w", resolver.ComponentNamePattern, index, err)
		}
		if ok := g.Match(componentName); ok {
			return resolver.Repository, nil
		}
	}

	return nil, repository.ErrNotFound
}
