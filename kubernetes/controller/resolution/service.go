package resolution

import (
	"context"
	"errors"
	"fmt"
	"sync"

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
		inProgress:  sync.Map{},
	}
}

// lookupRequest contains all the relevant information for a lookup provided to a doWork.
type lookupRequest struct {
	// this is the request context. The worker has its own context, however, it needs to stop
	// the current work if the request cancelled the context. But it mustn't stop itself.
	ctx  context.Context
	opts ResolveOptions
	cfg  configuration.Configuration
	key  string
}

// Result contains the result of a resolution including any errors that might have occurred.
type Result struct {
	key    string
	result *ResolveResult
	err    error
}

// Resolver provides implementation for component version resolution using a doWork pool. The async resolution
// is none blocking so the controller can return once the resolution is done.
type Resolver struct {
	client client.Reader
	logger logr.Logger
	pm     *manager.PluginManager
	sf     *singleflight.Group
	opts   ResolverOptions

	cache       Cache
	lookupQueue chan *lookupRequest
	inProgress  sync.Map // tracks keys currently being resolved
}

// ResolveComponentVersion resolves a component version with deduplication and async queuing.
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

	// TODO: The cache needs a TTL.
	// check cache (fast path)
	if cached, ok := r.cache.Get(key); ok {
		// check the result and if it's an error, return that immediately and delete the result.
		CacheHitCounterTotal.WithLabelValues(opts.Component, opts.Version).Inc()
		if cached.err != nil {
			r.cache.Delete(key)
			return nil, cached.err
		}
		return cached.result, nil
	}

	CacheMissCounterTotal.WithLabelValues(opts.Component, opts.Version).Inc()

	// singleflight deduplicates concurrent requests for the same key
	v, err, shared := r.sf.Do(key, func() (any, error) {
		return r.enqueueResolution(ctx, *opts, key, *cfg)
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

func (r *Resolver) enqueueResolution(ctx context.Context, opts ResolveOptions, key string, cfg configuration.Configuration) (*ResolveResult, error) {
	// Check cache (another goroutine may have populated it) while getting here however unlikely
	if cached, ok := r.cache.Get(key); ok {
		CacheHitCounterTotal.WithLabelValues(opts.Component, opts.Version).Inc()
		if cached.err != nil {
			r.cache.Delete(key)
			return nil, cached.err
		}

		return cached.result, nil
	}

	// Check if already in progress... it might be a long-running process and was requested rapidly again
	// in that case, we don't want to enqueue the task again. The reconciler will have to requeue, wait, and
	// try for the result again.
	if _, inProgress := r.inProgress.Load(key); inProgress {
		return nil, ErrResolutionInProgress
	}

	// Deep copy the repository spec to avoid concurrent write/read races during hashing.
	// The worker will mutate the spec (SetType during resolve), while other goroutines may read it in (buildCacheKey)
	optsCopy := opts
	optsCopy.RepositorySpec = opts.RepositorySpec.DeepCopyTyped()

	// Try to enqueue the request
	select {
	case r.lookupQueue <- &lookupRequest{
		ctx:  ctx,
		opts: optsCopy,
		cfg:  cfg,
		key:  key,
	}:
		QueueSizeGauge.Set(float64(len(r.lookupQueue)))
		r.logger.V(1).Info("enqueued lookup request", "component", opts.Component, "version", opts.Version)

		// Mark as in progress ONLY after it has been successfully enqueued, otherwise we would need to remove it.
		// Since we are in-flight, de-duplications for the same config should happen so the queue
		// should only have one item in it even if it was called with the same config twice.
		// The worker, once finished, will remove this task from the inProgress queue.
		// Like in a restaurant, an order is placed, and the cook, when done with the food, marks the food as completed.
		r.inProgress.Store(key, true)

		// return a known error so the controller can requeue after a while and try to get a result that time.
		return nil, ErrResolutionInProgress
	default:
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
		ConfigHash: cfg.Hash,
	}

	return result, nil
}
