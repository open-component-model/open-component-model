package resolution

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/utils/lru"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/resolution/workerpool"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/setup"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/verification"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/pkg/configuration"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ErrResolutionInProgress is returned when a component version is being resolved in the background.
var ErrResolutionInProgress = workerpool.ErrResolutionInProgress

// NewResolver creates a new component version resolver.
// The returned worker pool must be started separately by adding it to the manager.
func NewResolver(logger *logr.Logger, workerPool *workerpool.WorkerPool) *Resolver {
	resolver := &Resolver{
		logger:     logger,
		workerPool: workerPool,
		repoCache:  lru.New(100),
	}

	return resolver
}

// WorkerPool returns the underlying worker pool for event source creation.
func (r *Resolver) WorkerPool() *workerpool.WorkerPool {
	return r.workerPool
}

// Resolver provides implementation for component version resolution using a worker pool. The async resolution
// is non-blocking so the controller can return once the resolution is done.
type Resolver struct {
	logger     *logr.Logger
	workerPool *workerpool.WorkerPool
	repoCache  *lru.Cache
}

// RepositoryOptions contains all the options the resolution service requires to perform a resolve operation.
type RepositoryOptions struct {
	RepositorySpec runtime.Typed
	Configuration  *configuration.Configuration
	RequesterFunc  func() workerpool.RequesterInfo
	// Verifications are used to verify against component version signatures and used a cache key.
	Verifications []verification.Verification
	// Digest is used to verify the integrity of a referenced component version and is used as part of the cache key.
	Digest        *v2.Digest
	PluginManager *manager.PluginManager
}

// NewCacheBackedRepository creates a new cache-backed repository wrapper.
// It creates a provider that resolves the appropriate repository for each component based on:
// 1. Path matcher resolvers from OCM configuration (if configured)
// 2. The provided RepositorySpec as a fallback
func (r *Resolver) NewCacheBackedRepository(ctx context.Context, opts *RepositoryOptions) (*CacheBackedRepository, error) {
	cfg := opts.Configuration
	if opts.PluginManager == nil {
		return nil, fmt.Errorf("plugin manager is required")
	}

	requesterFunc := opts.RequesterFunc
	if requesterFunc == nil {
		requesterFunc = func() workerpool.RequesterInfo {
			return workerpool.RequesterInfo{}
		}
	}
	baseRepoSpec := opts.RepositorySpec
	if baseRepoSpec == nil {
		return nil, fmt.Errorf("base repository spec is required")
	}
	var configHash []byte
	if cfg != nil {
		configHash = cfg.Hash
	}
	cacheKey, err := buildRepoCacheKey(configHash, baseRepoSpec)
	if err != nil {
		return nil, fmt.Errorf("failed to build repository cache key: %w", err)
	}
	var provider resolvers.ComponentVersionRepositoryResolver
	if cached, ok := r.repoCache.Get(cacheKey); ok {
		provider = cached.(resolvers.ComponentVersionRepositoryResolver)
	} else {
		provider, err = r.createResolver(ctx, opts.RepositorySpec, cfg, opts.PluginManager)
		if err != nil {
			return nil, fmt.Errorf("failed to create provider: %w", err)
		}
		r.repoCache.Add(cacheKey, provider)
	}

	return &CacheBackedRepository{
		logger:          r.logger,
		resolver:        provider,
		cfg:             cfg,
		workerPool:      r.workerPool,
		requesterFunc:   requesterFunc,
		baseRepoSpec:    baseRepoSpec,
		verifications:   opts.Verifications,
		digest:          opts.Digest,
		signingRegistry: opts.PluginManager.SigningRegistry,
	}, nil
}

// createResolver creates a resolver based on the configuration.
// The resolver handles resolving the appropriate repository for each component.
func (r *Resolver) createResolver(ctx context.Context, spec runtime.Typed, cfg *configuration.Configuration, pm *manager.PluginManager) (resolvers.ComponentVersionRepositoryResolver, error) {
	if spec == nil {
		return nil, fmt.Errorf("repository spec is required")
	}

	opts := resolvers.Options{
		RepoProvider: pm.ComponentVersionRepositoryRegistry,
	}

	if cfg != nil {
		credGraph, err := setup.NewCredentialGraph(ctx, cfg.Config, setup.CredentialGraphOptions{
			PluginManager: pm,
			Logger:        r.logger,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create credential graph: %w", err)
		}
		r.logger.V(1).Info("resolved credential graph")
		opts.CredentialGraph = credGraph

		fallbackResolvers, pathMatchers, err := resolvers.ExtractResolvers(cfg.Config, ocirepository.Scheme)
		if err != nil {
			return nil, err
		}
		opts.FallbackResolvers = fallbackResolvers
		opts.PathMatchers = pathMatchers
	}

	return resolvers.New(ctx, opts, spec)
}
