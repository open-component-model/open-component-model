package resolution

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/go-logr/logr"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/internal/configuration"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution/workerpool"
	"ocm.software/open-component-model/kubernetes/controller/internal/setup"
)

// CacheBackedRepository provides a cache-backed implementation of repository.ComponentVersionRepository.
// It uses a provider to resolve the appropriate repository for each component, enabling pattern-based
// routing where different components can be served by different repositories.
// This is a READ-ONLY cache. Writing operations are delegated directly to the resolved repository.
type CacheBackedRepository struct {
	provider   setup.ComponentVersionRepositoryForComponentProvider
	cfg        *configuration.Configuration
	workerPool *workerpool.WorkerPool
	logger     *logr.Logger
	// requesterFunc is used to get a collection of types.NamespacedNames that want to listen to reconcile events
	// that the cache handles. Upon an event (resolution complete regardless of outcome) all objects in this
	// list are notified which will trigger a new reconcile event.
	requesterFunc func() workerpool.RequesterInfo
}

var _ repository.ComponentVersionRepository = (*CacheBackedRepository)(nil)

// newCacheBackedRepository creates a new CacheBackedRepository instance.
func newCacheBackedRepository(
	logger *logr.Logger,
	provider setup.ComponentVersionRepositoryForComponentProvider,
	cfg *configuration.Configuration,
	wp *workerpool.WorkerPool,
	requesterFunc func() workerpool.RequesterInfo,
) *CacheBackedRepository {
	return &CacheBackedRepository{
		logger:        logger,
		provider:      provider,
		cfg:           cfg,
		workerPool:    wp,
		requesterFunc: requesterFunc,
	}
}

// AddComponentVersion adds a component version to the underlying repository.
func (c *CacheBackedRepository) AddComponentVersion(ctx context.Context, desc *descriptor.Descriptor) error {
	repo, err := c.provider.GetComponentVersionRepositoryForComponent(ctx, desc.Component.Name, desc.Component.Version)
	if err != nil {
		return fmt.Errorf("failed to get repository for component %s:%s: %w", desc.Component.Name, desc.Component.Version, err)
	}
	return repo.AddComponentVersion(ctx, desc)
}

// GetComponentVersion retrieves a component version, using the cache when possible.
// This function is async. First call to this function will return a resolution.ErrResolutionInProgress error.
// Second call, once the resolution succeeds, will return a cached result with a default TTL.
func (c *CacheBackedRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	// Get the resolved repository spec for cache key generation
	resolvedSpec, err := c.provider.GetRepositorySpecForComponent(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve repository spec for component %s:%s: %w", component, version, err)
	}

	var configHash []byte
	if c.cfg != nil {
		configHash = c.cfg.Hash
	}

	// Create cache key using the resolved spec
	keyFunc := func() (string, error) {
		return buildCacheKey(configHash, resolvedSpec, component, version)
	}

	// Get the actual repository for this component
	repo, err := c.provider.GetComponentVersionRepositoryForComponent(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository for component %s:%s: %w", component, version, err)
	}

	wpOpts := workerpool.ResolveOptions{
		Component:  component,
		Version:    version,
		Repository: repo,
		KeyFunc:    keyFunc,
		Requester:  c.requesterFunc(),
	}

	result, err := c.workerPool.GetComponentVersion(ctx, wpOpts)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// ListComponentVersions lists all versions of a component.
// We never cache this call because it needs to return actual, existing versions on each call.
func (c *CacheBackedRepository) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	repo, err := c.provider.GetComponentVersionRepositoryForComponent(ctx, component, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get repository for component %s: %w", component, err)
	}
	return repo.ListComponentVersions(ctx, component)
}

// AddLocalResource adds a local resource to the underlying repository.
func (c *CacheBackedRepository) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	repo, err := c.provider.GetComponentVersionRepositoryForComponent(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository for component %s:%s: %w", component, version, err)
	}
	return repo.AddLocalResource(ctx, component, version, res, content)
}

// GetLocalResource retrieves a local resource from the underlying repository.
func (c *CacheBackedRepository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	repo, err := c.provider.GetComponentVersionRepositoryForComponent(ctx, component, version)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get repository for component %s:%s: %w", component, version, err)
	}
	return repo.GetLocalResource(ctx, component, version, identity)
}

// AddLocalSource adds a local source to the underlying repository.
func (c *CacheBackedRepository) AddLocalSource(ctx context.Context, component, version string, src *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	repo, err := c.provider.GetComponentVersionRepositoryForComponent(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository for component %s:%s: %w", component, version, err)
	}
	return repo.AddLocalSource(ctx, component, version, src, content)
}

// GetLocalSource retrieves a local source from the underlying repository.
func (c *CacheBackedRepository) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	repo, err := c.provider.GetComponentVersionRepositoryForComponent(ctx, component, version)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get repository for component %s:%s: %w", component, version, err)
	}
	return repo.GetLocalSource(ctx, component, version, identity)
}

// CheckHealth calls health check on the underlying repository.
// Since we use a provider, we get the repository for a placeholder component to check health.
// Returns nil if the repository does not support health checking.
func (c *CacheBackedRepository) CheckHealth(ctx context.Context) error {
	// For health check, we use a placeholder - the actual repo returned should support health check
	// if any of the underlying repos do. This is a limitation but acceptable for health checks.
	repo, err := c.provider.GetComponentVersionRepositoryForComponent(ctx, "health-check", "v0.0.0")
	if err != nil {
		// If we can't get a repository, we can't check health
		c.logger.V(1).Info("could not get repository for health check", "error", err)
		return nil
	}

	checkable, ok := repo.(repository.HealthCheckable)
	if !ok {
		c.logger.V(1).Info("repository is not health-checkable")
		return nil
	}

	return checkable.CheckHealth(ctx)
}

// buildCacheKey generates a cache key from the configuration hash, repository spec, component, and version.
// It canonicalizes the repository spec using JCS (RFC 8785) before hashing to ensure consistent keys
// regardless of field ordering in the JSON representation.
func buildCacheKey(configHash []byte, repoSpec runtime.Typed, component, version string) (string, error) {
	repoJSON, err := json.Marshal(repoSpec)
	if err != nil {
		return "", fmt.Errorf("failed to marshal repository spec: %w", err)
	}

	canonicalJSON, err := jsoncanonicalizer.Transform(repoJSON)
	if err != nil {
		return "", fmt.Errorf("failed to canonicalize repository spec: %w", err)
	}

	hasher := fnv.New64a()
	// can safely ignore because fnv.Write never actually returns an error
	_, _ = hasher.Write(configHash)
	_, _ = hasher.Write(canonicalJSON)
	_, _ = hasher.Write([]byte(component))
	_, _ = hasher.Write([]byte(version))

	return fmt.Sprintf("%016x", hasher.Sum64()), nil
}
