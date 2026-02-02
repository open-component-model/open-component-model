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
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/internal/configuration"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution/workerpool"
)

// CacheBackedRepository provides a cache-backed implementation of repository.ComponentVersionRepository.
// It uses a provider to resolve the appropriate repository for each component, enabling pattern-based
// routing where different components can be served by different repositories.
// This is a READ-ONLY cache. Writing operations are delegated directly to the resolved repository.
type CacheBackedRepository struct {
	resolver   resolvers.ComponentVersionRepositoryResolver
	cfg        *configuration.Configuration
	workerPool *workerpool.WorkerPool
	logger     *logr.Logger
	// requesterFunc is used to get a collection of types.NamespacedNames that want to listen to reconcile events
	// that the cache handles. Upon an event (resolution complete regardless of outcome) all objects in this
	// list are notified which will trigger a new reconcile event.
	requesterFunc func() workerpool.RequesterInfo
	baseRepoSpec  runtime.Typed
}

var _ repository.ComponentVersionRepository = (*CacheBackedRepository)(nil)

// newCacheBackedRepository creates a new CacheBackedRepository instance.
func newCacheBackedRepository(
	logger *logr.Logger,
	resolver resolvers.ComponentVersionRepositoryResolver,
	cfg *configuration.Configuration,
	wp *workerpool.WorkerPool,
	requesterFunc func() workerpool.RequesterInfo,
	baseRepoSpec runtime.Typed,
) *CacheBackedRepository {
	return &CacheBackedRepository{
		logger:        logger,
		resolver:      resolver,
		cfg:           cfg,
		workerPool:    wp,
		requesterFunc: requesterFunc,
		baseRepoSpec:  baseRepoSpec,
	}
}

// AddComponentVersion adds a component version to the underlying repository.
func (c *CacheBackedRepository) AddComponentVersion(ctx context.Context, desc *descriptor.Descriptor) error {
	repo, err := c.resolver.GetComponentVersionRepositoryForComponent(ctx, desc.Component.Name, desc.Component.Version)
	if err != nil {
		return fmt.Errorf("failed to get repository for component %s:%s: %w", desc.Component.Name, desc.Component.Version, err)
	}
	return repo.AddComponentVersion(ctx, desc)
}

// GetComponentVersion retrieves a component version, using the cache when possible.
// This function is async. First call to this function will return a resolution.ErrResolutionInProgress error.
// Second call, once the resolution succeeds, will return a cached result with a default TTL.
func (c *CacheBackedRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	var configHash []byte
	if c.cfg != nil {
		configHash = c.cfg.Hash
	}

	keyFunc := func() (string, error) {
		// Build cache key based on configuration hash, repository spec, component, and version.
		// The baseRepoSpec is not necessarily the repository used to resolve the component.
		// The actual repository is determined by the providers resolver
		// configuration (which is represented through the config hash) and
		// the base repository.
		return buildCacheKey(configHash, c.baseRepoSpec, component, version)
	}

	repo, err := c.resolver.GetComponentVersionRepositoryForComponent(ctx, component, version)
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
	repo, err := c.resolver.GetComponentVersionRepositoryForComponent(ctx, component, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get repository for component %s: %w", component, err)
	}
	return repo.ListComponentVersions(ctx, component)
}

// AddLocalResource adds a local resource to the underlying repository.
func (c *CacheBackedRepository) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	repo, err := c.resolver.GetComponentVersionRepositoryForComponent(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository for component %s:%s: %w", component, version, err)
	}
	return repo.AddLocalResource(ctx, component, version, res, content)
}

// GetLocalResource retrieves a local resource from the underlying repository.
func (c *CacheBackedRepository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	repo, err := c.resolver.GetComponentVersionRepositoryForComponent(ctx, component, version)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get repository for component %s:%s: %w", component, version, err)
	}
	return repo.GetLocalResource(ctx, component, version, identity)
}

// AddLocalSource adds a local source to the underlying repository.
func (c *CacheBackedRepository) AddLocalSource(ctx context.Context, component, version string, src *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	repo, err := c.resolver.GetComponentVersionRepositoryForComponent(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository for component %s:%s: %w", component, version, err)
	}
	return repo.AddLocalSource(ctx, component, version, src, content)
}

// GetLocalSource retrieves a local source from the underlying repository.
func (c *CacheBackedRepository) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	repo, err := c.resolver.GetComponentVersionRepositoryForComponent(ctx, component, version)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get repository for component %s:%s: %w", component, version, err)
	}
	return repo.GetLocalSource(ctx, component, version, identity)
}

// CheckHealth calls health check on the underlying base repository.
// Returns nil if the repository does not support health checking.
func (c *CacheBackedRepository) CheckHealth(ctx context.Context) error {
	repo, err := c.resolver.GetComponentVersionRepositoryForSpecification(ctx, c.baseRepoSpec)
	if err != nil {
		return fmt.Errorf("failed to get repository for health check: %w", err)
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

// buildCacheKey generates a cache key from the configuration hash, repository spec.
// It canonicalizes the repository spec using JCS (RFC 8785) before hashing to ensure consistent keys
// regardless of field ordering in the JSON representation.
func buildRepoCacheKey(configHash []byte, repoSpec runtime.Typed) (string, error) {
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

	return fmt.Sprintf("%016x", hasher.Sum64()), nil
}
