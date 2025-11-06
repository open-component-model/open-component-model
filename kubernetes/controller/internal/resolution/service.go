package resolution

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/internal/configuration"
	"ocm.software/open-component-model/kubernetes/controller/internal/plugins"
	"ocm.software/open-component-model/kubernetes/controller/internal/setup"
)

// ErrResolutionInProgress is returned when a component version is being resolved in the background.
var ErrResolutionInProgress = errors.New("component version resolution in progress")

type ComponentVersionResolver interface {
	ResolveComponentVersion(ctx context.Context, opts *ResolveOptions) (*ResolveResult, error)
}

// NewResolver creates a new component version resolver.
// The returned worker pool must be started separately by adding it to the manager.
func NewResolver(client client.Reader, logger logr.Logger, workerPool *WorkerPool, pluginManager *plugins.PluginManager) *Resolver {
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
	workerPool    *WorkerPool
	pluginManager *plugins.PluginManager
}

type CacheBackedRepository struct {
	opts       ResolveOptions
	cfg        *configuration.Configuration
	workerPool *WorkerPool
	repo       repository.ComponentVersionRepository
}

func (c *CacheBackedRepository) AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error {
	return c.repo.AddComponentVersion(ctx, descriptor)
}

func (c *CacheBackedRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	opts := c.opts
	opts.Component = component
	opts.Version = version

	var configHash []byte
	if c.cfg != nil {
		configHash = c.cfg.Hash
	}

	key, err := buildCacheKey(configHash, c.opts.RepositorySpec, component, version)
	if err != nil {
		return nil, fmt.Errorf("failed to build cache key: %w", err)
	}

	result, err := c.workerPool.GetComponentVersion(ctx, key, opts, c.repo, configHash)
	if err != nil {
		return nil, err
	}

	return result.Descriptor, nil
}

func (c *CacheBackedRepository) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	opts := c.opts
	opts.Component = component

	var configHash []byte
	if c.cfg != nil {
		configHash = c.cfg.Hash
	}

	key, err := buildCacheKey(configHash, c.opts.RepositorySpec, component, "")
	if err != nil {
		return nil, fmt.Errorf("failed to build cache key: %w", err)
	}

	result, err := c.workerPool.ListComponentVersions(ctx, key, opts, c.repo, configHash)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (c *CacheBackedRepository) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	// TODO: Eventually cache these as well.
	return c.repo.AddLocalResource(ctx, component, version, res, content)
}

func (c *CacheBackedRepository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	return c.repo.GetLocalResource(ctx, component, version, identity)
}

func (c *CacheBackedRepository) AddLocalSource(ctx context.Context, component, version string, src *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	return c.repo.AddLocalSource(ctx, component, version, src, content)
}

func (c *CacheBackedRepository) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	return c.repo.GetLocalSource(ctx, component, version, identity)
}

var _ repository.ComponentVersionRepository = (*CacheBackedRepository)(nil)

//
//// ResolveComponentVersion resolves a component version using the cache-backed repository.
//// This is a convenience method that implements the ComponentVersionResolver interface.
//func (r *Resolver) ResolveComponentVersion(ctx context.Context, opts *ResolveOptions) (*ResolveResult, error) {
//	if err := r.validateOptions(opts); err != nil {
//		return nil, err
//	}
//
//	// Deep copy the repository spec early to avoid races during hashing/marshaling.
//	optsCopy := ResolveOptions{
//		RepositorySpec:    opts.RepositorySpec.DeepCopyTyped(),
//		OCMConfigurations: slices.Clone(opts.OCMConfigurations),
//		Namespace:         opts.Namespace,
//		Component:         opts.Component,
//		Version:           opts.Version,
//	}
//
//	// Load OCM configurations
//	cfg, err := configuration.LoadConfigurations(ctx, r.client, optsCopy.Namespace, optsCopy.OCMConfigurations)
//	if err != nil || cfg == nil {
//		return nil, fmt.Errorf("failed to load OCM configurations: %w", err)
//	}
//
//	repo, err := r.createRepository(ctx, opts, cfg)
//	if err != nil {
//		return nil, err
//	}
//
//	key, err := buildCacheKey(cfg.Hash, optsCopy.RepositorySpec, optsCopy.Component, optsCopy.Version)
//	if err != nil {
//		return nil, fmt.Errorf("failed to build cache key: %w", err)
//	}
//
//	return r.workerPool.GetComponentVersion(ctx, key, optsCopy, repo, cfg.Hash)
//}

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

	return &CacheBackedRepository{
		opts:       optsCopy,
		cfg:        cfg,
		workerPool: r.workerPool,
		repo:       repo,
	}, nil
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
