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
)

// CacheBackedRepository provides a cache-backed implementation of repository.ComponentVersionRepository.
// It wraps a real repository and uses a worker pool to handle concurrent access with caching.
// This is a READ-ONLY cache. Writing operations are not cached.
type CacheBackedRepository struct {
	spec          runtime.Typed
	cfg           *configuration.Configuration
	workerPool    *workerpool.WorkerPool
	repo          repository.ComponentVersionRepository
	logger        *logr.Logger
	requesterFunc func() workerpool.RequesterInfo
}

var _ repository.ComponentVersionRepository = (*CacheBackedRepository)(nil)

// newCacheBackedRepository creates a new CacheBackedRepository instance.
func newCacheBackedRepository(logger *logr.Logger, spec runtime.Typed, cfg *configuration.Configuration, wp *workerpool.WorkerPool, repo repository.ComponentVersionRepository, requesterFunc func() workerpool.RequesterInfo) *CacheBackedRepository {
	return &CacheBackedRepository{
		logger:        logger,
		spec:          spec,
		cfg:           cfg,
		workerPool:    wp,
		repo:          repo,
		requesterFunc: requesterFunc,
	}
}

// AddComponentVersion adds a component version to the underlying repository.
func (c *CacheBackedRepository) AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error {
	return c.repo.AddComponentVersion(ctx, descriptor)
}

// GetComponentVersion retrieves a component version, using the cache when possible.
// This function is async. First call to this function will return a resolution.ErrResolutionInProgress error.
// Second call, once the resolution succeeds, will return a cached result with a default TTL.
func (c *CacheBackedRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	var configHash []byte
	if c.cfg != nil {
		configHash = c.cfg.Hash
	}

	// create our key function that the cache will use to determine the key for this request
	keyFunc := func() (string, error) {
		return buildCacheKey(configHash, c.spec, component, version)
	}

	wpOpts := workerpool.ResolveOptions{
		Component:  component,
		Version:    version,
		Repository: c.repo,
		KeyFunc:    keyFunc,
		Requester:  c.requesterFunc(),
	}

	result, err := c.workerPool.GetComponentVersion(ctx, wpOpts)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// ListComponentVersions lists all versions of a component, using the cache when possible.
// We never cache this call because it needs to return actual, existing versions on each call.
func (c *CacheBackedRepository) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	return c.repo.ListComponentVersions(ctx, component)
}

// AddLocalResource adds a local resource to the underlying repository.
func (c *CacheBackedRepository) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	return c.repo.AddLocalResource(ctx, component, version, res, content)
}

// GetLocalResource retrieves a local resource from the underlying repository.
func (c *CacheBackedRepository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	return c.repo.GetLocalResource(ctx, component, version, identity)
}

// AddLocalSource adds a local source to the underlying repository.
func (c *CacheBackedRepository) AddLocalSource(ctx context.Context, component, version string, src *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	return c.repo.AddLocalSource(ctx, component, version, src, content)
}

// GetLocalSource retrieves a local source from the underlying repository.
func (c *CacheBackedRepository) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	return c.repo.GetLocalSource(ctx, component, version, identity)
}

// CheckHealth calls health check on the underlying repository. Returns an error if the repository does not support
// health checking.
func (c *CacheBackedRepository) CheckHealth(ctx context.Context) error {
	checkable, ok := c.repo.(repository.HealthCheckable)
	if !ok {
		c.logger.V(1).Info("repository is not health-checkable", "repository", c.spec)

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
