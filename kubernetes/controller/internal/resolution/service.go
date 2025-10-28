package resolution

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/kubernetes/controller/internal/configuration"
)

// ErrResolutionInProgress is returned when a component version is being resolved in the background.
var ErrResolutionInProgress = errors.New("component version resolution in progress")

type ComponentVersionResolver interface {
	ResolveComponentVersion(ctx context.Context, opts *ResolveOptions) (*ResolveResult, error)
}

// NewResolver creates a new component version resolver.
// The returned worker pool must be started separately by adding it to the manager.
func NewResolver(client client.Reader, logger logr.Logger, workerPool *WorkerPool) *Resolver {
	if logger.GetSink() == nil {
		logger = logr.Discard()
	}

	resolver := &Resolver{
		client:     client,
		logger:     logger,
		workerPool: workerPool,
	}

	return resolver
}

// Resolver provides implementation for component version resolution using a worker pool. The async resolution
// is non-blocking so the controller can return once the resolution is done.
type Resolver struct {
	client     client.Reader
	logger     logr.Logger
	workerPool *WorkerPool
}

// ResolveComponentVersion resolves a component version with deduplication and async queuing.
func (r *Resolver) ResolveComponentVersion(ctx context.Context, opts *ResolveOptions) (*ResolveResult, error) {
	if err := r.validateOptions(opts); err != nil {
		return nil, err
	}

	// Deep copy the repository spec early to avoid races during hashing/marshaling.
	// Workers may mutate the spec (SetType during resolve), so we need our own copy.
	// Full deep copy to prevent pointer shares.
	optsCopy := ResolveOptions{
		RepositorySpec:    opts.RepositorySpec.DeepCopyTyped(),
		Component:         opts.Component,
		Version:           opts.Version,
		OCMConfigurations: slices.Clone(opts.OCMConfigurations),
		Namespace:         opts.Namespace,
	}

	// Load OCM configurations
	cfg, err := configuration.LoadConfigurations(ctx, r.client, optsCopy.Namespace, optsCopy.OCMConfigurations)
	if err != nil || cfg == nil {
		return nil, fmt.Errorf("failed to load OCM configurations: %w", err)
	}

	key, err := buildCacheKey(cfg.Hash, optsCopy.RepositorySpec, optsCopy.Component, optsCopy.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to build cache key: %w", err)
	}

	return r.workerPool.Resolve(ctx, key, optsCopy, cfg)
}

func (r *Resolver) validateOptions(opts *ResolveOptions) error {
	if opts == nil {
		return errors.New("resolve options cannot be nil")
	}

	if opts.RepositorySpec == nil {
		return errors.New("repository spec is required")
	}

	if opts.Component == "" {
		return errors.New("component name is required")
	}

	if opts.Version == "" {
		return errors.New("component version is required")
	}

	return nil
}
