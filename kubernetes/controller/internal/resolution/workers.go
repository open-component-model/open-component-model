package resolution

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/golang-lru/v2/expirable"
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
	// Cache for caching.
	Cache *expirable.LRU[string, *Result]
}

// WorkerPool manages a pool of workers that process work items concurrently.
type WorkerPool struct {
	opts      WorkerPoolOptions
	workQueue chan *WorkItem
	cache     *expirable.LRU[string, *Result]
	logger    logr.Logger
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
		opts:      opts,
		workQueue: make(chan *WorkItem, opts.QueueSize),
		cache:     opts.Cache,
		logger:    opts.Logger,
	}
}

// Start begins the worker pool and result collector.
// This method blocks until the context is cancelled to implement graceful shutdown.
func (wp *WorkerPool) Start(ctx context.Context) error {
	wp.logger.Info("starting worker pool", "workers", wp.opts.WorkerCount, "queueSize", wp.opts.QueueSize)

	// Start workers and collect their result channels (fan-out)
	workerChannels := make([]chan *Result, 0, wp.opts.WorkerCount)
	for i := range wp.opts.WorkerCount {
		workerChannels = append(workerChannels, wp.startWorker(ctx, i))
	}

	// Start result collector (fan-in the worker channels later)
	collectorDone := make(chan struct{})
	go func() {
		wp.resultCollector(ctx, workerChannels)
		close(collectorDone)
	}()

	// block until context is done to implement proper runnable.
	<-ctx.Done()
	wp.logger.Info("worker pool shutting down, draining queue")

	// close work queue to signal workers to stop
	close(wp.workQueue)

	// wait for all workers to finish and result collector to stop
	<-collectorDone

	wp.logger.Info("worker pool shutdown complete")
	return nil
}

// Resolve resolves a component version with caching and async queuing.
func (wp *WorkerPool) Resolve(ctx context.Context, key string, opts ResolveOptions, cfg *configuration.Configuration) (*ResolveResult, error) {
	// Check cache first (fast path)
	if cached, ok := wp.cache.Get(key); ok {
		CacheHitCounterTotal.WithLabelValues(opts.Component, opts.Version).Inc()
		if cached.err != nil {
			return nil, cached.err
		}
		return cached.result, nil
	}

	CacheMissCounterTotal.WithLabelValues(opts.Component, opts.Version).Inc()

	workItem := &WorkItem{
		Context: ctx,
		Key:     key,
		Opts:    opts,
		Cfg:     cfg,
	}

	select {
	// Try to enqueue the request. If it fails, we return a queue full error so the reconciler can try again later.
	case wp.workQueue <- workItem:
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
			case item, ok := <-wp.workQueue:
				if !ok {
					logger.V(1).Info("work queue closed, worker exiting")
					return
				}

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
					key:      item.Key,
					result:   result,
					err:      err,
					createAt: time.Now(),
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
		// once all workers are done, we clean up the mergeResults channel
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

			// store result in cache
			wp.cache.Add(res.key, res)
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
