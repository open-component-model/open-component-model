package resolution

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository"
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
func (r *Resolver) NewCacheBackedRepository(ctx context.Context, opts *RepositoryOptions) (*CacheBackedRepository, error) {
	// Load OCM configurations
	cfg, err := configuration.LoadConfigurations(ctx, r.client, opts.Namespace, opts.OCMConfigurations)
	if err != nil {
		return nil, fmt.Errorf("failed to load OCM configurations: %w", err)
	}

	repo, err := r.createRepository(ctx, opts.RepositorySpec, cfg)
	if err != nil {
		return nil, err
	}

	requesterFunc := opts.RequesterFunc
	if requesterFunc == nil {
		requesterFunc = func() workerpool.RequesterInfo {
			return workerpool.RequesterInfo{}
		}
	}

	return newCacheBackedRepository(r.logger, opts.RepositorySpec, cfg, r.workerPool, repo, requesterFunc), nil
}

func (r *Resolver) createRepository(ctx context.Context, spec runtime.Typed, cfg *configuration.Configuration) (repository.ComponentVersionRepository, error) {
	options := setup.RepositoryOptions{
		Registry: r.pluginManager.ComponentVersionRepositoryRegistry,
		Logger:   r.logger,
	}

	var credGraph credentials.Resolver
	if cfg != nil {
		var err error
		credGraph, err = setup.NewCredentialGraph(ctx, cfg.Config, setup.CredentialGraphOptions{
			PluginManager: r.pluginManager,
			Logger:        r.logger,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create credential graph: %w", err)
		}
		r.logger.V(1).Info("resolved credential graph")

		options.CredentialGraph = credGraph
		resolvers, err := setup.GetResolversV1Alpha1(cfg.Config)
		if err != nil {
			return nil, fmt.Errorf("failed to extract path matcher resolvers: %w", err)
		}

		if len(resolvers) > 0 {
			r.logger.V(1).Info("using path matcher resolvers for component resolution", "resolverCount", len(resolvers))
			if spec != nil {
				providerOpts := setup.ResolverProviderOptions{
					Registry:        r.pluginManager.ComponentVersionRepositoryRegistry,
					CredentialGraph: credGraph,
					Logger:          r.logger,
					Resolvers:       resolvers,
				}

				provider, err := setup.NewResolverProviderWithRepository(ctx, providerOpts, spec, nil)
				if err != nil {
					return nil, fmt.Errorf("failed to create resolver provider: %w", err)
				}

				return newResolverBackedRepository(provider), nil
			}

			providerOpts := setup.ResolverProviderOptions{
				Registry:        r.pluginManager.ComponentVersionRepositoryRegistry,
				CredentialGraph: credGraph,
				Logger:          r.logger,
				Resolvers:       resolvers,
			}

			provider, err := setup.NewResolverProvider(ctx, providerOpts)
			if err != nil {
				return nil, fmt.Errorf("failed to create resolver provider: %w", err)
			}

			return newResolverBackedRepository(provider), nil
		}

		r.logger.V(1).Info("no path matcher resolvers configured, using direct repository")
	}

	repo, err := setup.NewRepository(ctx, spec, options)
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}
	return repo, nil
}
