package resolution

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocirepository "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository/component/providers"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/configuration"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution/workerpool"
	"ocm.software/open-component-model/kubernetes/controller/internal/setup"
)

// ErrResolutionInProgress is returned when a component version is being resolved in the background.
var ErrResolutionInProgress = workerpool.ErrResolutionInProgress

// NewResolver creates a new component version resolver.
// The returned worker pool must be started separately by adding it to the manager.
func NewResolver(client client.Reader, logger *logr.Logger, workerPool *workerpool.WorkerPool, pluginManager *manager.PluginManager) *Resolver {
	resolver := &Resolver{
		client:        client,
		logger:        logger,
		workerPool:    workerPool,
		pluginManager: pluginManager,
		repoCache:     expirable.NewLRU[string, providers.ComponentVersionRepositoryForComponentProvider](0, nil, time.Minute*30),
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
	client        client.Reader
	logger        *logr.Logger
	workerPool    *workerpool.WorkerPool
	pluginManager *manager.PluginManager
	repoCache     *expirable.LRU[string, providers.ComponentVersionRepositoryForComponentProvider]
}

// RepositoryOptions contains all the options the resolution service requires to perform a resolve operation.
// The RepositorySpec, Component, Version, the accumulated configuration, the namespace for the resolved configuration.
type RepositoryOptions struct {
	RepositorySpec    runtime.Typed
	OCMConfigurations []v1alpha1.OCMConfiguration
	Namespace         string
	RequesterFunc     func() workerpool.RequesterInfo
}

// NewCacheBackedRepository creates a new cache-backed repository wrapper.
// It creates a provider that resolves the appropriate repository for each component based on:
// 1. Path matcher resolvers from OCM configuration (if configured)
// 2. The provided RepositorySpec as a fallback
func (r *Resolver) NewCacheBackedRepository(ctx context.Context, opts *RepositoryOptions) (*CacheBackedRepository, error) {
	cfg, err := configuration.LoadConfigurations(ctx, r.client, opts.Namespace, opts.OCMConfigurations)
	if err != nil {
		return nil, fmt.Errorf("failed to load OCM configurations: %w", err)
	}

	requesterFunc := opts.RequesterFunc
	if requesterFunc == nil {
		requesterFunc = func() workerpool.RequesterInfo {
			return workerpool.RequesterInfo{}
		}
	}
	baseRepoSpec := opts.RepositorySpec
	var configHash []byte
	if cfg != nil {
		configHash = cfg.Hash
	}
	cacheKey, err := buildRepoCacheKey(configHash, baseRepoSpec)
	if err != nil {
		return nil, fmt.Errorf("failed to build repository cache key: %w", err)
	}
	provider, ok := r.repoCache.Get(cacheKey)
	if !ok {
		provider, err = r.createProvider(ctx, opts.RepositorySpec, cfg)
		r.repoCache.Add(cacheKey, provider)
	}

	return newCacheBackedRepository(r.logger, provider, cfg, r.workerPool, requesterFunc, baseRepoSpec), nil
}

// createProvider creates a provider based on the configuration.
// The provider handles resolving the appropriate repository for each component.
func (r *Resolver) createProvider(ctx context.Context, spec runtime.Typed, cfg *configuration.Configuration) (providers.ComponentVersionRepositoryForComponentProvider, error) {
	if spec == nil {
		return nil, fmt.Errorf("repository spec is required")
	}

	opts := providers.Options{
		RepoProvider: r.pluginManager.ComponentVersionRepositoryRegistry,
	}

	if cfg != nil {
		credGraph, err := setup.NewCredentialGraph(ctx, cfg.Config, setup.CredentialGraphOptions{
			PluginManager: r.pluginManager,
			Logger:        r.logger,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create credential graph: %w", err)
		}
		r.logger.V(1).Info("resolved credential graph")
		opts.CredentialGraph = credGraph

		fallbackResolvers, pathMatchers, err := providers.ExtractResolvers(cfg.Config, ocirepository.Scheme)
		if err != nil {
			return nil, err
		}
		opts.FallbackResolvers = fallbackResolvers
		opts.PathMatchers = pathMatchers
	}

	return providers.New(ctx, opts, spec)
}
