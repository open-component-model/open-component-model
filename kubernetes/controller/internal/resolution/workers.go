package resolution

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/kubernetes/controller/internal/configuration"
	"ocm.software/open-component-model/kubernetes/controller/internal/plugins"
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
	PluginManager *plugins.PluginManager
	// Cache for caching.
	Cache *expirable.LRU[string, *Result]
}

// WorkerPool manages a pool of workers that process work items concurrently.
type WorkerPool struct {
	WorkerPoolOptions
	workQueue   chan *WorkItem
	inProgress  sync.Map // map[string]struct{} - tracks keys currently being processed
	workersDone sync.WaitGroup
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
		WorkerPoolOptions: opts,
		workQueue:         make(chan *WorkItem, opts.QueueSize),
	}
}

// Start begins the worker pool.
// This method blocks until the context is cancelled to implement graceful shutdown.
func (wp *WorkerPool) Start(ctx context.Context) error {
	wp.Logger.Info("starting worker pool", "workers", wp.WorkerCount, "queueSize", wp.QueueSize)

	for i := range wp.WorkerCount {
		wp.workersDone.Add(1)
		go wp.worker(ctx, i)
	}

	// wait for context cancellation
	<-ctx.Done()
	wp.Logger.Info("worker pool shutting down, draining queue")

	// close work queue to signal workers to stop
	close(wp.workQueue)

	// wait for all workers to finish
	wp.workersDone.Wait()

	wp.Logger.Info("worker pool shutdown complete")
	return nil
}

// Resolve resolves a component version with caching and async queuing.
func (wp *WorkerPool) Resolve(ctx context.Context, key string, opts ResolveOptions, cfg *configuration.Configuration) (*ResolveResult, error) {
	if cached, ok := wp.Cache.Get(key); ok {
		CacheHitCounterTotal.WithLabelValues(opts.Component, opts.Version).Inc()
		if cached.err != nil {
			return nil, cached.err
		}
		return cached.result, nil
	}

	CacheMissCounterTotal.WithLabelValues(opts.Component, opts.Version).Inc()

	if _, exists := wp.inProgress.LoadOrStore(key, struct{}{}); exists {
		wp.Logger.V(1).Info("resolution already in progress", "component", opts.Component, "version", opts.Version)
		return nil, ErrResolutionInProgress
	}

	InProgressGauge.Inc()

	workItem := &WorkItem{
		Context: ctx,
		Key:     key,
		Opts:    opts,
		Cfg:     cfg,
	}

	select {
	case wp.workQueue <- workItem:
		QueueSizeGauge.Set(float64(len(wp.workQueue)))
		wp.Logger.V(1).Info("enqueued resolution request", "component", opts.Component, "version", opts.Version)
		return nil, ErrResolutionInProgress
	default:
		// failed, decrement in-progress and decrement the gauge.
		wp.inProgress.Delete(key)
		InProgressGauge.Dec()
		if len(wp.workQueue) == wp.QueueSize {
			return nil, fmt.Errorf("work queue is full, cannot enqueue request for %s:%s", opts.Component, opts.Version)
		}

		return nil, fmt.Errorf("failed to enqueue resolution request for %s:%s", opts.Component, opts.Version)
	}
}

// worker is the main worker loop that processes work items and updates the cache directly.
func (wp *WorkerPool) worker(ctx context.Context, id int) {
	defer wp.workersDone.Done()
	logger := wp.Logger.WithValues("worker", id)
	logger.V(1).Info("worker started")
	defer logger.V(1).Info("worker stopped")

	for {
		select {
		case <-ctx.Done():
			logger.V(1).Info("worker stopped due to context cancellation")
			return
		case item, ok := <-wp.workQueue:
			if !ok {
				// the channel was closed; most likely the controller is shutting down and
				// the starting context was cancelled which closes the worker queue.
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

			wp.Cache.Add(item.Key, &Result{
				result: result,
				err:    err,
			})

			// we are done, remove in-progress
			wp.inProgress.Delete(item.Key)
			InProgressGauge.Dec()
		}
	}
}

// resolve performs the actual component version resolution.
func (wp *WorkerPool) resolve(ctx context.Context, opts *ResolveOptions, cfg *configuration.Configuration) (*ResolveResult, error) {
	pm := wp.PluginManager.PluginManager
	if pm == nil {
		return nil, fmt.Errorf("plugin manager is nil")
	}

	credGraph, err := setup.NewCredentialGraph(ctx, cfg.Config, setup.CredentialGraphOptions{
		PluginManager: pm,
		Logger:        wp.Logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create credential graph: %w", err)
	}
	wp.Logger.V(1).Info("resolved credential graph")

	repo, err := setup.NewRepository(ctx, opts.RepositorySpec, setup.RepositoryOptions{
		PluginManager:   pm,
		CredentialGraph: credGraph,
		Logger:          wp.Logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}
	wp.Logger.V(1).Info("new repository created")

	descriptor, err := repo.GetComponentVersion(ctx, opts.Component, opts.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to get component version %s:%s: %w", opts.Component, opts.Version, err)
	}
	wp.Logger.V(1).Info("resolved component version", "component", opts.Component, "version", opts.Version, "descriptor", descriptor)

	result := &ResolveResult{
		Descriptor: descriptor,
		Repository: repo,
		ConfigHash: cfg.Hash,
	}

	return result, nil
}
