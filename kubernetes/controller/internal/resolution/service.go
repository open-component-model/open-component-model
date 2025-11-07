package resolution

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/kubernetes/controller/internal/configuration"
	"ocm.software/open-component-model/kubernetes/controller/internal/plugins"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution/workerpool"
	"ocm.software/open-component-model/kubernetes/controller/internal/setup"
)

// ErrResolutionInProgress is returned when a component version is being resolved in the background.
var ErrResolutionInProgress = workerpool.ErrResolutionInProgress

type ComponentVersionResolver interface {
	ResolveComponentVersion(ctx context.Context, opts *ResolveOptions) (*ResolveResult, error)
}

// NewResolver creates a new component version resolver.
// The returned worker pool must be started separately by adding it to the manager.
func NewResolver(client client.Reader, logger logr.Logger, workerPool *workerpool.WorkerPool, pluginManager *plugins.PluginManager) *Resolver {
	if logger.GetSink() == nil {
		logger = logr.Discard()
	}

	resolver := &Resolver{
		client:        client,
		logger:        logger,
		workerPool:    workerPool,
		pluginManager: pluginManager,
	}

	return resolver
}

// Resolver provides implementation for component version resolution using a worker pool. The async resolution
// is non-blocking so the controller can return once the resolution is done.
type Resolver struct {
	client        client.Reader
	logger        logr.Logger
	workerPool    *workerpool.WorkerPool
	pluginManager *plugins.PluginManager
}

// NewCacheBackedRepository creates a new cache-backed repository wrapper.
func (r *Resolver) NewCacheBackedRepository(ctx context.Context, opts *ResolveOptions) (*CacheBackedRepository, error) {
	if err := r.validateOptions(opts); err != nil {
		return nil, err
	}

	// Deep copy the repository spec early to avoid races during hashing/marshaling.
	// Workers may mutate the spec (SetType during resolve), so we need our own copy.
	// Full deep copy to prevent pointer shares.
	optsCopy := ResolveOptions{
		RepositorySpec:    opts.RepositorySpec.DeepCopyTyped(),
		OCMConfigurations: slices.Clone(opts.OCMConfigurations),
		Namespace:         opts.Namespace,
	}

	// Load OCM configurations
	cfg, err := configuration.LoadConfigurations(ctx, r.client, optsCopy.Namespace, optsCopy.OCMConfigurations)
	if err != nil {
		return nil, fmt.Errorf("failed to load OCM configurations: %w", err)
	}

	repo, err := r.createRepository(ctx, opts, cfg)
	if err != nil {
		return nil, err
	}

	return newCacheBackedRepository(optsCopy, cfg, r.workerPool, repo), nil
}

func (r *Resolver) validateOptions(opts *ResolveOptions) error {
	if opts == nil {
		return errors.New("resolve options cannot be nil")
	}

	if opts.RepositorySpec == nil {
		return errors.New("repository spec is required")
	}

	return nil
}

func (r *Resolver) createRepository(ctx context.Context, opts *ResolveOptions, cfg *configuration.Configuration) (repository.ComponentVersionRepository, error) {
	pm := r.pluginManager.PluginManager
	if pm == nil {
		return nil, fmt.Errorf("plugin manager is nil")
	}

	options := setup.RepositoryOptions{
		PluginManager: pm,
		Logger:        r.logger,
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

		options.CredentialGraph = credGraph
	}

	repo, err := setup.NewRepository(ctx, opts.RepositorySpec, options)
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}
	return repo, nil
}
