package resolution

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/internal/setup"
)

// resolverBackedRepository implements repository.ComponentVersionRepository by dynamically
// resolving the appropriate repository for each component using a resolver provider.
// This enables pattern-based routing where different components can be served by different repositories.
type resolverBackedRepository struct {
	provider setup.ComponentVersionRepositoryForComponentProvider
}

var _ repository.ComponentVersionRepository = (*resolverBackedRepository)(nil)

// newResolverBackedRepository creates a new resolver-backed repository.
func newResolverBackedRepository(provider setup.ComponentVersionRepositoryForComponentProvider) *resolverBackedRepository {
	return &resolverBackedRepository{
		provider: provider,
	}
}

// GetComponentVersion retrieves a component version by first resolving the appropriate repository
// for the component, then fetching the version from that repository.
func (r *resolverBackedRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	repo, err := r.provider.GetComponentVersionRepositoryForComponent(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("resolving repository for component %s:%s failed: %w", component, version, err)
	}

	return repo.GetComponentVersion(ctx, component, version)
}

// ListComponentVersions lists all versions of a component by first resolving the appropriate repository.
func (r *resolverBackedRepository) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	// For listing, we need to resolve with an empty version
	repo, err := r.provider.GetComponentVersionRepositoryForComponent(ctx, component, "")
	if err != nil {
		return nil, fmt.Errorf("resolving repository for component %s failed: %w", component, err)
	}

	return repo.ListComponentVersions(ctx, component)
}

// AddComponentVersion adds a component version by first resolving the appropriate repository.
func (r *resolverBackedRepository) AddComponentVersion(ctx context.Context, desc *descriptor.Descriptor) error {
	repo, err := r.provider.GetComponentVersionRepositoryForComponent(ctx, desc.Component.Name, desc.Component.Version)
	if err != nil {
		return fmt.Errorf("resolving repository for component %s:%s failed: %w", desc.Component.Name, desc.Component.Version, err)
	}

	return repo.AddComponentVersion(ctx, desc)
}

// AddLocalResource adds a local resource by first resolving the appropriate repository.
func (r *resolverBackedRepository) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	repo, err := r.provider.GetComponentVersionRepositoryForComponent(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("resolving repository for component %s:%s failed: %w", component, version, err)
	}

	return repo.AddLocalResource(ctx, component, version, res, content)
}

// GetLocalResource retrieves a local resource by first resolving the appropriate repository.
func (r *resolverBackedRepository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	repo, err := r.provider.GetComponentVersionRepositoryForComponent(ctx, component, version)
	if err != nil {
		return nil, nil, fmt.Errorf("resolving repository for component %s:%s failed: %w", component, version, err)
	}

	return repo.GetLocalResource(ctx, component, version, identity)
}

// AddLocalSource adds a local source by first resolving the appropriate repository.
func (r *resolverBackedRepository) AddLocalSource(ctx context.Context, component, version string, src *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	repo, err := r.provider.GetComponentVersionRepositoryForComponent(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("resolving repository for component %s:%s failed: %w", component, version, err)
	}

	return repo.AddLocalSource(ctx, component, version, src, content)
}

// GetLocalSource retrieves a local source by first resolving the appropriate repository.
func (r *resolverBackedRepository) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	repo, err := r.provider.GetComponentVersionRepositoryForComponent(ctx, component, version)
	if err != nil {
		return nil, nil, fmt.Errorf("resolving repository for component %s:%s failed: %w", component, version, err)
	}

	return repo.GetLocalSource(ctx, component, version, identity)
}
