package workerpool

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"sigs.k8s.io/controller-runtime/pkg/client"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

// WorkItemType indicates the type of work to be performed.
type WorkItemType int

const (
	WorkItemTypeGetComponentVersion WorkItemType = iota
	WorkItemTypeListComponentVersions
)

// ResolveOptions contains all the options the resolution service requires to perform a resolve operation.
type ResolveOptions struct {
	RepositorySpec    runtime.Typed
	Component         string
	Version           string
	OCMConfigurations []v1alpha1.OCMConfiguration
	Namespace         string
}

// ResolveResult contains the descriptor, repository and the computed hash of the config for further processing.
type ResolveResult struct {
	Descriptor *descriptor.Descriptor
	Repository repository.ComponentVersionRepository
	ConfigHash []byte
}

// Result contains the result of a resolution including any errors that might have occurred.
type Result struct {
	Value any
	Error error
}

// WorkItem represents a single work item to be processed by the worker pool.
type WorkItem struct {
	// Type indicates the type of work to be performed.
	Type WorkItemType
	// Context for the work item - workers respect this context for cancellation.
	Context context.Context
	// Key is a unique identifier for this work item.
	Key string
	// Opts contains the resolve options.
	Opts ResolveOptions
	// Repository belonging to this worker pool to work with.
	Repository repository.ComponentVersionRepository
	// ConfigHash is the hash of the configuration used to create the repository.
	ConfigHash []byte
}

// PoolOptions configures the worker pool.
type PoolOptions struct {
	// WorkerCount is the number of concurrent workers.
	WorkerCount int
	// QueueSize is the size of the work queue buffer.
	QueueSize int
	// Logger for the worker pool.
	Logger logr.Logger
	// Client for Kubernetes API access.
	Client client.Reader
	// Cache for caching.
	Cache *expirable.LRU[string, *Result]
}

// WorkerPool manages a pool of workers that process work items concurrently.
type WorkerPool struct {
	PoolOptions
	workQueue   chan *WorkItem
	inProgress  sync.Map // map[string]struct{} - tracks keys currently being processed
	workersDone sync.WaitGroup
}

// ErrResolutionInProgress is returned when a component version is being resolved in the background.
var ErrResolutionInProgress = fmt.Errorf("component version resolution in progress")

// NewWorkerPool creates a new worker pool.
func NewWorkerPool(opts PoolOptions) *WorkerPool {
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
		PoolOptions: opts,
		workQueue:   make(chan *WorkItem, opts.QueueSize),
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

// GetComponentVersion retrieves a component version using the worker pool and cache.
func (wp *WorkerPool) GetComponentVersion(ctx context.Context, key string, opts ResolveOptions, repo repository.ComponentVersionRepository, configHash []byte) (*ResolveResult, error) {
	return enqueueWorkItem[*ResolveResult](ctx, wp, key, opts, repo, configHash, WorkItemTypeGetComponentVersion)
}

// ListComponentVersions lists component versions using the worker pool and cache.
// TODO: this is obsolete
func (wp *WorkerPool) ListComponentVersions(ctx context.Context, key string, opts ResolveOptions, repo repository.ComponentVersionRepository, configHash []byte) ([]string, error) {
	return enqueueWorkItem[[]string](ctx, wp, key, opts, repo, configHash, WorkItemTypeListComponentVersions)
}

func enqueueWorkItem[T any](ctx context.Context, wp *WorkerPool, key string, opts ResolveOptions, repo repository.ComponentVersionRepository, configHash []byte, workItemType WorkItemType) (result T, _ error) {
	if cached, ok := wp.Cache.Get(key); ok {
		CacheHitCounterTotal.WithLabelValues(opts.Component, opts.Version).Inc()
		if cached.Error != nil {
			return result, cached.Error
		}

		res, ok := cached.Value.(T)
		if !ok {
			return result, fmt.Errorf("unable to assert cache value for key %s", key)
		}

		return res, nil
	}

	CacheMissCounterTotal.WithLabelValues(opts.Component, "latest").Inc()

	if _, exists := wp.inProgress.LoadOrStore(key, struct{}{}); exists {
		wp.Logger.V(1).Info("resolution already in progress", "component", opts.Component, "version", opts.Version)
		return result, ErrResolutionInProgress
	}

	InProgressGauge.Inc()

	workItem := &WorkItem{
		Type:       workItemType,
		Context:    ctx,
		Key:        key,
		Repository: repo,
		Opts:       opts,
		ConfigHash: configHash,
	}

	select {
	case wp.workQueue <- workItem:
		QueueSizeGauge.Set(float64(len(wp.workQueue)))
		wp.Logger.V(1).Info("enqueued request", "component", opts.Component)
		return result, ErrResolutionInProgress
	default:
		// failed, decrement in-progress and decrement the gauge.
		wp.inProgress.Delete(key)
		InProgressGauge.Dec()
		if len(wp.workQueue) == wp.QueueSize {
			return result, fmt.Errorf("work queue is full, cannot enqueue request for %s", opts.Component)
		}

		return result, fmt.Errorf("failed to enqueue resolution request for %s", opts.Component)
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

			switch item.Type {
			case WorkItemTypeGetComponentVersion:
				wp.handleWorkItemWithHash(wp.getComponentVersion, logger, item)
			case WorkItemTypeListComponentVersions:
				wp.handleWorkItem(wp.listComponentVersions, logger, item)
			default:
				logger.Error(fmt.Errorf("unknown work item type: %d", item.Type), "skipping work item")
			}
		}
	}
}

// workFunc is the signature for functions that process work items.
type workFunc func(ctx context.Context, item *WorkItem) (any, error)

func (wp *WorkerPool) handleWorkItem(f workFunc, logger logr.Logger, item *WorkItem) {
	logger.V(1).Info("processing work item", "key", item.Key)

	start := time.Now()
	result, err := f(item.Context, item)
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
		Value: result,
		Error: err,
	})

	// we are done, remove in-progress
	wp.inProgress.Delete(item.Key)
	InProgressGauge.Dec()
}

func (wp *WorkerPool) handleWorkItemWithHash(f workFunc, logger logr.Logger, item *WorkItem) {
	logger.V(1).Info("processing work item", "key", item.Key)

	start := time.Now()
	result, err := f(item.Context, item)
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

		// Add ConfigHash to the result if it's a ResolveResult
		if resolveResult, ok := result.(*ResolveResult); ok {
			resolveResult.ConfigHash = item.ConfigHash
		}
	}

	wp.Cache.Add(item.Key, &Result{
		Value: result,
		Error: err,
	})

	// we are done, remove in-progress
	wp.inProgress.Delete(item.Key)
	InProgressGauge.Dec()
}

// getComponentVersion performs the actual component version resolution.
func (wp *WorkerPool) getComponentVersion(ctx context.Context, item *WorkItem) (any, error) {
	descriptor, err := item.Repository.GetComponentVersion(ctx, item.Opts.Component, item.Opts.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to get component version %s:%s: %w", item.Opts.Component, item.Opts.Version, err)
	}
	wp.Logger.V(1).Info("resolved component version", "component", item.Opts.Component, "version", item.Opts.Version, "descriptor", descriptor)

	result := &ResolveResult{
		Descriptor: descriptor,
		Repository: item.Repository,
		ConfigHash: item.ConfigHash,
	}

	return result, nil
}

// listComponentVersions lists all versions for a component.
func (wp *WorkerPool) listComponentVersions(ctx context.Context, item *WorkItem) (any, error) {
	versions, err := item.Repository.ListComponentVersions(ctx, item.Opts.Component)
	if err != nil {
		return nil, fmt.Errorf("failed to list component versions for %s: %w", item.Opts.Component, err)
	}
	wp.Logger.V(1).Info("listed component versions", "component", item.Opts.Component, "count", len(versions))

	return versions, nil
}
