package resolution

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/go-logr/logr"
	"golang.org/x/sync/singleflight"
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
		sf:         &singleflight.Group{},
	}

	return resolver
}

// Resolver provides implementation for component version resolution using a worker pool. The async resolution
// is non-blocking so the controller can return once the resolution is done.
type Resolver struct {
	client     client.Reader
	logger     logr.Logger
	workerPool *WorkerPool
	sf         *singleflight.Group
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

	// Singleflight ensures only one goroutine enqueues/checks for a given key.
	// Duplicate callers wait here until the first caller completes.
	var v any
	singleFlightResult := r.sf.DoChan(key, func() (any, error) {
		return r.workerPool.Resolve(ctx, key, optsCopy, cfg)
	})
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("context was cancelled during singleflight resolve call: %w", ctx.Err())
	case res := <-singleFlightResult:
		if res.Err != nil {
			return nil, fmt.Errorf("failed to resolve component version: %w", res.Err)
		}

		if res.Shared {
			CacheShareCounterTotal.WithLabelValues(optsCopy.Component, optsCopy.Version).Inc()
		}

		v = res.Val
	}

	// v will be nil if we got ErrResolutionInProgress
	if v == nil {
		return nil, ErrResolutionInProgress
	}

	result, ok := v.(*ResolveResult)
	if !ok {
		// Not possible, but defensive programming.
		return nil, fmt.Errorf("failed to convert result to *ResolveResult, was: %T", v)
	}

	return result, nil
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
