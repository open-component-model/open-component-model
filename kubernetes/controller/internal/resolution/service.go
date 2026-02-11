package resolution

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/utils/lru"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/configuration"
	"ocm.software/open-component-model/kubernetes/controller/internal/ocm"
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
		repoCache:     lru.New(100),
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
	repoCache     *lru.Cache
}

// RepositoryOptions contains all the options the resolution service requires to perform a resolve operation.
// The RepositorySpec, Component, Version, the accumulated configuration, the namespace for the resolved configuration.
type RepositoryOptions struct {
	RepositorySpec    runtime.Typed
	OCMConfigurations []v1alpha1.OCMConfiguration
	Namespace         string
	RequesterFunc     func() workerpool.RequesterInfo
	// Verifications are used to create a cache key to distinguish between verified and unverified component versions
	Verifications []ocm.Verification
	// Digest is used to create a cache key for component versions from component references resolved in the resource
	// controller. It is used to distinguish between integrity-checked and unchecked component versions. The integrity
	// can only be checked if the component reference provides a digest specification.
	Digest          *v2.Digest
	SigningRegistry *signinghandler.SigningRegistry
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
		provider, err = r.createResolver(ctx, opts.RepositorySpec, cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create provider: %w", err)
		}
		r.repoCache.Add(cacheKey, provider)
	}

	return newCacheBackedRepository(r.logger, provider, cfg, r.workerPool, requesterFunc, baseRepoSpec, opts.Verifications, opts.Digest, opts.SigningRegistry), nil
}

// createResolver creates a resolver based on the configuration.
// The resolver handles resolving the appropriate repository for each component.
func (r *Resolver) createResolver(ctx context.Context, spec runtime.Typed, cfg *configuration.Configuration) (resolvers.ComponentVersionRepositoryResolver, error) {
	if spec == nil {
		return nil, fmt.Errorf("repository spec is required")
	}

	opts := resolvers.Options{
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

		fallbackResolvers, pathMatchers, err := resolvers.ExtractResolvers(cfg.Config, ocirepository.Scheme)
		if err != nil {
			return nil, err
		}
		opts.FallbackResolvers = fallbackResolvers
		opts.PathMatchers = pathMatchers
	}

	return resolvers.New(ctx, opts, spec)
}
