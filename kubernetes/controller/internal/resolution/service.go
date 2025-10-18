package resolution

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/sync/singleflight"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/kubernetes/controller/internal/configuration"
	"ocm.software/open-component-model/kubernetes/controller/internal/setup"
)

// ErrResolutionInProgress is returned when a component version is being resolved in the background.
var ErrResolutionInProgress = errors.New("component version resolution in progress")

type ComponentVersionResolver interface {
	ResolveComponentVersion(ctx context.Context, opts *ResolveOptions) (*ResolveResult, error)
	// Start implements manager.Runnable.
	Start(ctx context.Context) error
}

// ResolverOptions configures the resolution service.
type ResolverOptions struct {
	// WorkerCount is the number of concurrent workers for resolution.
	WorkerCount int
	// QueueSize is the size of the lookup queue buffer.
	QueueSize int
}

// DefaultResolverOptions returns default resolver options.
func DefaultResolverOptions() ResolverOptions {
	return ResolverOptions{
		WorkerCount: 10,
		QueueSize:   100,
	}
}

// NewResolver creates a new component version resolver.
func NewResolver(client client.Reader, logger logr.Logger, pm *manager.PluginManager, opts ResolverOptions) *Resolver {
	if logger.GetSink() == nil {
		logger = logr.Discard()
	}

	if opts.WorkerCount <= 0 {
		opts.WorkerCount = 10
	}

	if opts.QueueSize <= 0 {
		opts.QueueSize = 100
	}

	return &Resolver{
		client:      client,
		logger:      logger,
		cache:       NewInMemoryCache(),
		pm:          pm,
		sf:          &singleflight.Group{},
		lookupQueue: make(chan *lookupRequest, opts.QueueSize),
		opts:        opts,
		inProgress:  make(map[string]struct{}),
	}
}

// lookupRequest contains all the relevant information for a lookup provided to a worker.
// TODO: Need to figure out how to update a result once it's in the cache with an Error.
type lookupRequest struct {
	ctx  context.Context
	opts *ResolveOptions
	cfg  *configuration.Configuration
	key  cacheKey
}

// Resolver provides implementation for component version resolution using a worker pool. The async resolution
// is none blocking so the controller can return once the resolution is done.
type Resolver struct {
	client client.Reader
	logger logr.Logger
	pm     *manager.PluginManager
	mu     sync.RWMutex
	sf     *singleflight.Group
	opts   ResolverOptions

	cache       Cache
	lookupQueue chan *lookupRequest
	inProgress  map[string]struct{} // tracks keys currently being resolved
}

// ResolveComponentVersion resolves a component version with deduplication and async queuing.
//
// Flow:
// 1. Check cache hit: return immediately (check for Error field in result)
// 2. Check cache miss: increment cache miss metric
// 3. Use singleflight to deduplicate concurrent requests for the same cache key
//   - Only ONE goroutine per unique key enters the singleflight function
//   - All other concurrent requests for the same key wait and share the result
//     4. Inside singleflight (winner goroutine):
//     a. Double-check cache (may have been populated by previous winner)
//     b. Check if already in progress (enqueued by previous winner)
//   - Mark as in-progress
//   - Enqueue to worker pool queue → return ErrResolutionInProgress
//   - Workers resolve in background and update cache (including errors)
func (r *Resolver) ResolveComponentVersion(ctx context.Context, opts *ResolveOptions) (*ResolveResult, error) {
	if err := r.validateOptions(opts); err != nil {
		return nil, err
	}

	// for now, we don't allow nil configs...
	cfg, err := configuration.LoadConfigurations(ctx, r.client, opts.Namespace, opts.OCMConfigurations)
	if err != nil || cfg == nil {
		return nil, fmt.Errorf("failed to load OCM configurations: %w", err)
	}

	key, err := buildCacheKey(cfg.Hash, opts.RepositorySpec, opts.Component, opts.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to build cache key: %w", err)
	}

	// check cache (fast path)
	if cached, ok := r.cache.Get(key.String()); ok {
		// check the result and if it's an error, return that immediately and delete the result.
		CacheHitCounterTotal.WithLabelValues(opts.Component, opts.Version).Inc()
		if cached.Error != nil {
			r.cache.Delete(key.String())
			return nil, cached.Error
		}
		return cached, nil
	}

	CacheMissCounterTotal.WithLabelValues(opts.Component, opts.Version).Inc()

	// singleflight deduplicates concurrent requests for the same key
	v, err, shared := r.sf.Do(key.String(), func() (any, error) {
		return r.enqueueResolution(ctx, opts, key, cfg)
	})
	if err != nil {
		return nil, err
	}

	if shared {
		CacheShareCounterTotal.WithLabelValues(opts.Component, opts.Version).Inc()
	}

	// this is unlikely as we always return an error or a result, but nevertheless...
	if v == nil {
		return nil, ErrResolutionInProgress
	}

	result := v.(*ResolveResult)
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

func (r *Resolver) enqueueResolution(ctx context.Context, opts *ResolveOptions, key cacheKey, cfg *configuration.Configuration) (*ResolveResult, error) {
	// check cache (another goroutine may have populated it) while getting here however unlikely
	if cached, ok := r.cache.Get(key.String()); ok {
		CacheHitCounterTotal.WithLabelValues(opts.Component, opts.Version).Inc()
		return cached, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, inProgress := r.inProgress[key.String()]; inProgress {
		return nil, ErrResolutionInProgress
	}

	r.inProgress[key.String()] = struct{}{}
	InProgressGauge.Set(float64(len(r.inProgress)))

	select {
	case r.lookupQueue <- &lookupRequest{
		ctx:  ctx,
		opts: opts,
		cfg:  cfg,
		key:  key,
	}:
		QueueSizeGauge.Set(float64(len(r.lookupQueue)))
		r.logger.V(1).Info("enqueued lookup request", "component", opts.Component, "version", opts.Version)
		return nil, ErrResolutionInProgress
	default:
		// TODO: what should happen if the queue is full?
		delete(r.inProgress, key.String())
		InProgressGauge.Set(float64(len(r.inProgress)))
		return nil, fmt.Errorf("lookup queue is full, cannot enqueue request for %s:%s", opts.Component, opts.Version)
	}
}

// resolve performs the actual component version resolution.
func (r *Resolver) resolve(ctx context.Context, opts *ResolveOptions, cfg *configuration.Configuration) (*ResolveResult, error) {
	credGraph, err := setup.NewCredentialGraph(ctx, cfg.Config, setup.CredentialGraphOptions{
		PluginManager: r.pm,
		Logger:        r.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create credential graph: %w", err)
	}

	// TODO: Maybe return the repo... maybe it's not enough even that?
	// Ocm context derived from the spec maybe that's even needed...
	// We really need resource access for downloading later.
	repo, err := setup.NewRepository(ctx, opts.RepositorySpec, setup.RepositoryOptions{
		PluginManager:   r.pm,
		CredentialGraph: credGraph,
		Logger:          r.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}

	descriptor, err := repo.GetComponentVersion(ctx, opts.Component, opts.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to get component version %s:%s: %w", opts.Component, opts.Version, err)
	}

	// TODO: Also return the repository that was resolved?
	// Because maybe we need the access?
	// If we want to do some download...
	result := &ResolveResult{
		Descriptor: descriptor,
		Repository: repo,
		Metadata: ResolveMetadata{
			ResolvedAt: time.Now(),
			ConfigHash: cfg.Hash,
		},
	}

	return result, nil
}
