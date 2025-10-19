package resolution

import (
	"context"
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

// WorkItem represents a single work item to be processed by the worker pool.
type WorkItem struct {
	// Context for the work item - workers respect this context for cancellation.
	Context context.Context
	// Key is a unique identifier for this work item.
	Key string
	// Opts contains the resolve options.
	Opts ResolveOptions
	// Cfg contains the configuration.
	Cfg *configuration.Configuration
}

// WorkerPoolOptions configures the worker pool.
type WorkerPoolOptions struct {
	// WorkerCount is the number of concurrent workers.
	WorkerCount int
	// QueueSize is the size of the work queue buffer.
	QueueSize int
	// Logger for the worker pool.
	Logger logr.Logger
	// Client for Kubernetes API access.
	Client client.Reader
	// PluginManager for OCM operations.
	PluginManager *manager.PluginManager
}

// WorkerPool manages a pool of workers that process work items concurrently.
type WorkerPool struct {
	opts       WorkerPoolOptions
	workQueue  chan *WorkItem
	inProgress sync.Map
	cache      Cache
	sf         *singleflight.Group
	logger     logr.Logger
}

// Result contains the result of a resolution including any errors that might have occurred.
type Result struct {
	key    string
	result *ResolveResult
	err    error
}

// NewWorkerPool creates a new worker pool.
func NewWorkerPool(opts WorkerPoolOptions) *WorkerPool {
	if opts.Logger.GetSink() == nil {
		opts.Logger = logr.Discard()
	}

	if opts.WorkerCount <= 0 {
		opts.WorkerCount = 10
	}

	if opts.QueueSize <= 0 {
		opts.QueueSize = 100
	}

	return &WorkerPool{
		opts:       opts,
		workQueue:  make(chan *WorkItem, opts.QueueSize),
		inProgress: sync.Map{},
		cache:      NewInMemoryCache(),
		sf:         &singleflight.Group{},
		logger:     opts.Logger,
	}
}

// Start begins the worker pool and result collector.
func (wp *WorkerPool) Start(ctx context.Context) error {
	wp.logger.Info("starting worker pool", "workers", wp.opts.WorkerCount, "queueSize", wp.opts.QueueSize)

	// Start workers and collect their result channels (fan-out)
	workerChannels := make([]chan *Result, 0, wp.opts.WorkerCount)
	for i := range wp.opts.WorkerCount {
		workerChannels = append(workerChannels, wp.startWorker(ctx, i))
	}

	go wp.resultCollector(ctx, workerChannels)

	return nil
}

// Resolve resolves a component version with caching, deduplication, and async queuing.
func (wp *WorkerPool) Resolve(ctx context.Context, key string, opts ResolveOptions, cfg *configuration.Configuration) (*ResolveResult, error) {
	// check cache (fast path)
	if cached, ok := wp.cache.Get(key); ok {
		CacheHitCounterTotal.WithLabelValues(opts.Component, opts.Version).Inc()
		if cached.err != nil {
			wp.cache.Delete(key)
			return nil, cached.err
		}
		return cached.result, nil
	}

	CacheMissCounterTotal.WithLabelValues(opts.Component, opts.Version).Inc()

	// Singleflight deduplicates concurrent requests for the same key
	v, err, shared := wp.sf.Do(key, func() (any, error) {
		return wp.enqueueResolution(ctx, opts, key, cfg)
	})
	if err != nil {
		return nil, err
	}

	if shared {
		CacheShareCounterTotal.WithLabelValues(opts.Component, opts.Version).Inc()
	}

	// This is unlikely as we always return an error or a result, but nevertheless...
	if v == nil {
		return nil, ErrResolutionInProgress
	}

	result := v.(*ResolveResult)
	return result, nil
}

// enqueueResolution enqueues a resolution request to the worker pool.
func (wp *WorkerPool) enqueueResolution(ctx context.Context, opts ResolveOptions, key string, cfg *configuration.Configuration) (*ResolveResult, error) {
	// Check cache (another goroutine may have populated it)
	if cached, ok := wp.cache.Get(key); ok {
		CacheHitCounterTotal.WithLabelValues(opts.Component, opts.Version).Inc()
		if cached.err != nil {
			wp.cache.Delete(key)
			return nil, cached.err
		}
		return cached.result, nil
	}

	// Check if already in progress
	if _, inProgress := wp.inProgress.Load(key); inProgress {
		return nil, ErrResolutionInProgress
	}

	// Try to enqueue the request (opts already has a deep-copied RepositorySpec from Resolver)
	workItem := &WorkItem{
		Context: ctx,
		Key:     key,
		Opts:    opts,
		Cfg:     cfg,
	}

	select {
	case wp.workQueue <- workItem:
		wp.inProgress.Store(key, true)
		QueueSizeGauge.Set(float64(len(wp.workQueue)))
		wp.logger.V(1).Info("enqueued resolution request", "component", opts.Component, "version", opts.Version)
		return nil, ErrResolutionInProgress
	default:
		return nil, fmt.Errorf("lookup queue is full, cannot enqueue request for %s:%s", opts.Component, opts.Version)
	}
}

// startWorker creates a worker's result channel and starts the worker in a goroutine.
// Returns the channel that the worker will send results to.
func (wp *WorkerPool) startWorker(ctx context.Context, id int) chan *Result {
	resultChan := make(chan *Result)
	logger := wp.logger.WithValues("worker", id)

	go func() {
		defer close(resultChan)
		defer logger.V(1).Info("worker stopped", "id", id)

		for {
			select {
			case <-ctx.Done():
				logger.V(1).Info("worker stopped due to context cancellation")
				return
			case item := <-wp.workQueue:
				QueueSizeGauge.Set(float64(len(wp.workQueue)))

				logger.V(1).Info("processing work item", "key", item.Key)

				start := time.Now()
				result, err := wp.resolve(item.Context, &item.Opts, item.Cfg)
				duration := time.Since(start).Seconds()

				// Track metrics
				ResolutionDurationHistogram.WithLabelValues(item.Opts.Component, item.Opts.Version).Observe(duration)

				if err != nil {
					logger.Error(err, "failed to process work item",
						"component", item.Opts.Component,
						"version", item.Opts.Version,
						"duration", duration)
				} else {
					logger.V(1).Info("processed work item",
						"component", item.Opts.Component,
						"version", item.Opts.Version,
						"duration", duration)
				}

				// Send result to worker's dedicated result channel
				resultChan <- &Result{
					key:    item.Key,
					result: result,
					err:    err,
				}
			}
		}
	}()

	return resultChan
}

// resultCollector processes results from multiple worker channels and updates the cache.
func (wp *WorkerPool) resultCollector(ctx context.Context, workerChannels []chan *Result) {
	logger := wp.logger.WithValues("component", "result-collector")
	logger.V(1).Info("result collector started", "workerCount", len(workerChannels))

	// Merge all worker channels into a single channel (fan-in)
	mergedResults := make(chan *Result)
	wg := &sync.WaitGroup{}
	wg.Add(len(workerChannels))
	for _, ch := range workerChannels {
		go func() {
			for res := range ch {
				mergedResults <- res
			}
			wg.Done()
		}()
	}

	go func() {
		// Close the merged channel when all workers exit
		wg.Wait()
		close(mergedResults)
	}()

	for {
		select {
		case <-ctx.Done():
			logger.V(1).Info("result collector stopped")
			return
		case res, ok := <-mergedResults:
			if !ok {
				logger.V(1).Info("all workers exited, result collector stopping")
				return
			}

			// Store result in cache
			wp.cache.Set(res.key, res)

			// Clear singleflight to prevent memory leak
			wp.sf.Forget(res.key)

			// Mark work as done
			wp.inProgress.Delete(res.key)
		}
	}
}

// resolve performs the actual component version resolution.
func (wp *WorkerPool) resolve(ctx context.Context, opts *ResolveOptions, cfg *configuration.Configuration) (*ResolveResult, error) {
	credGraph, err := setup.NewCredentialGraph(ctx, cfg.Config, setup.CredentialGraphOptions{
		PluginManager: wp.opts.PluginManager,
		Logger:        wp.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create credential graph: %w", err)
	}

	repo, err := setup.NewRepository(ctx, opts.RepositorySpec, setup.RepositoryOptions{
		PluginManager:   wp.opts.PluginManager,
		CredentialGraph: credGraph,
		Logger:          wp.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}

	descriptor, err := repo.GetComponentVersion(ctx, opts.Component, opts.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to get component version %s:%s: %w", opts.Component, opts.Version, err)
	}

	result := &ResolveResult{
		Descriptor: descriptor,
		Repository: repo,
		ConfigHash: cfg.Hash,
	}

	return result, nil
}
