package resolution

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/plugin/manager"
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

	provider, err := r.createProvider(ctx, opts.RepositorySpec, cfg)
	if err != nil {
		return nil, err
	}

	requesterFunc := opts.RequesterFunc
	if requesterFunc == nil {
		requesterFunc = func() workerpool.RequesterInfo {
			return workerpool.RequesterInfo{}
		}
	}

	return newCacheBackedRepository(r.logger, provider, cfg, r.workerPool, requesterFunc), nil
}

// createProvider creates a ComponentVersionRepositoryForComponentProvider based on the configuration.
// The provider handles resolving the appropriate repository for each component.
func (r *Resolver) createProvider(ctx context.Context, spec runtime.Typed, cfg *configuration.Configuration) (setup.ComponentVersionRepositoryForComponentProvider, error) {
	if spec == nil {
		return nil, fmt.Errorf("repository spec is required")
	}

	providerOpts := setup.ResolverProviderOptions{
		Registry: r.pluginManager.ComponentVersionRepositoryRegistry,
		Logger:   r.logger,
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
		providerOpts.CredentialGraph = credGraph
		resolvers, err := setup.GetResolversV1Alpha1(cfg.Config)
		if err != nil {
			return nil, fmt.Errorf("failed to extract path matcher resolvers: %w", err)
		}
		providerOpts.Resolvers = resolvers

		fallbackResolvers, err := setup.GetResolvers(cfg.Config)
		if err != nil {
			return nil, fmt.Errorf("failed to extract fallback resolvers: %w", err)
		}
		providerOpts.FallbackResolvers = fallbackResolvers

		if len(resolvers) > 0 {
			r.logger.V(1).Info("using path matcher resolvers for component resolution", "resolverCount", len(resolvers))
		}
		if len(fallbackResolvers) > 0 {
			r.logger.V(1).Info("using deprecated fallback resolvers for component resolution", "resolverCount", len(fallbackResolvers))
		}
	}

	return setup.NewResolverProviderWithRepository(ctx, providerOpts, spec)
}
