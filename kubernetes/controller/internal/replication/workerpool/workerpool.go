package workerpool

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"

	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

// ErrTransferInProgress is returned by [WorkerPool.Submit] when a transfer for
// the given key has been accepted and is queued or running. The reconciler
// treats it as a signal to exit and wait for the completion event.
var ErrTransferInProgress = errors.New("component version transfer in progress")

// ErrQueueFull is returned by [WorkerPool.Submit] when the bounded work queue
// has no free slot. The reconciler should requeue with backoff.
var ErrQueueFull = errors.New("transfer work queue is full")

// ErrPoolShuttingDown is returned by [WorkerPool.Submit] once the pool has begun
// shutting down. No new transfers are accepted; the reconciler should stop and
// let the manager restart drive a fresh reconcile on the next leader.
var ErrPoolShuttingDown = errors.New("transfer worker pool is shutting down")

// RequesterInfo identifies the object that requested a transfer and is notified
// via the completion event when the transfer finishes.
type RequesterInfo struct {
	NamespacedName types.NamespacedName
}

// SubmitOptions describes a transfer to submit to the pool.
type SubmitOptions struct {
	// Key uniquely identifies the transfer. The Replication CR UID is used so
	// burst reconciles for the same object map onto a single in-flight item.
	Key string
	// Stamp is the opaque content identity of this unit of work. The pool does
	// not interpret it; it only matches it, in [WorkerPool.Result], against the
	// stamp the caller currently wants, dropping a stale outcome. The reconciler
	// derives it from what defines the transfer (source digest, target, transfer
	// config), so a result produced for an older source is discarded once the
	// source has moved on.
	Stamp string
	// Requester is notified via the completion event when the transfer finishes.
	Requester RequesterInfo
	// TGD is the in-memory transformation graph definition built outside of this pool.
	TGD *transformv1alpha1.TransformationGraphDefinition
	// Builder builds and runs the executable graph from the TGD.
	Builder GraphBuilder
}

// Result is the outcome of a transfer. A nil Error means success.
type Result struct {
	// Stamp echoes the content identity the transfer was submitted with.
	// [WorkerPool.Result] matches it against the caller's desired stamp and
	// drops the result on mismatch, so a stale outcome never reaches the caller.
	Stamp string
	// Error is nil on success.
	Error error
}

// PoolOptions configures the worker pool.
type PoolOptions struct {
	// WorkerCount is the number of concurrent workers.
	WorkerCount int
	// QueueSize is the size of the work queue buffer.
	QueueSize int
	// SubscriberBufferSize is the buffer size for each subscriber's event
	// channel. A larger buffer reduces the probability of dropped events under
	// load.
	SubscriberBufferSize int
	// ShutdownTimeout bounds how long Start waits for workers to drain on
	// shutdown. Defaults to 5s.
	ShutdownTimeout time.Duration
	// Logger for the worker pool.
	Logger *logr.Logger
}

// inflight tracks the state of a queued or running transfer.
type inflight struct {
	// requesters are all objects waiting on this key, notified on completion.
	requesters []RequesterInfo
	// cancel cancels the per-key context, used for the deletion drain and on
	// pool shutdown.
	cancel context.CancelFunc
}

// workItem is the internal unit processed by a worker.
type workItem struct {
	key     string
	stamp   string
	tgd     *transformv1alpha1.TransformationGraphDefinition
	builder GraphBuilder
	// ctx is the per-key context, derived from the pool base context.
	ctx context.Context
}

// WorkerPool runs component version transfers asynchronously. Submission is
// non-blocking and backed by a bounded queue. Completions are broadcast to
// subscribers, which retrigger reconciliation.
type WorkerPool struct {
	PoolOptions

	// workQueue is intentionally never closed. Submit sends to it concurrently, and a
	// `send` on a closed channel panics; closing also buys nothing, because workers
	// terminate on ctx.Done, not on a queue close. Never `range` over this channel
	// since with no close signal a range would block forever.
	workQueue chan *workItem

	// mu guards inProgress, results, and closed.
	mu         sync.Mutex
	inProgress map[string]*inflight
	results    map[string]*Result
	// closed is set when shutdown begins. It gates Submit so no work is enqueued
	// after the workers stop which also guards a send-on-closed-channel panic.
	closed bool

	subscribersMu sync.RWMutex
	subscribers   []chan []RequesterInfo

	// baseCtx is the parent of every per-key context. It is canceled on
	// shutdown so all in-flight transfers stop.
	baseCtx    context.Context
	baseCancel context.CancelFunc

	workersDone sync.WaitGroup
}

const (
	defaultWorkerCount          = 5
	defaultQueueSize            = 1000
	defaultSubscriberBufferSize = 100
	defaultShutdownTimeout      = 5 * time.Second
)

// NewWorkerPool creates a new transfer worker pool.
func NewWorkerPool(opts PoolOptions) *WorkerPool {
	if opts.WorkerCount <= 0 {
		opts.WorkerCount = defaultWorkerCount
	}

	if opts.QueueSize <= 0 {
		opts.QueueSize = defaultQueueSize
	}

	if opts.SubscriberBufferSize <= 0 {
		opts.SubscriberBufferSize = defaultSubscriberBufferSize
	}

	if opts.ShutdownTimeout <= 0 {
		opts.ShutdownTimeout = defaultShutdownTimeout
	}

	baseCtx, baseCancel := context.WithCancel(context.Background()) //nolint:gosec // baseCancel is called later.

	return &WorkerPool{
		PoolOptions: opts,
		workQueue:   make(chan *workItem, opts.QueueSize),
		inProgress:  make(map[string]*inflight),
		results:     make(map[string]*Result),
		subscribers: make([]chan []RequesterInfo, 0),
		baseCtx:     baseCtx,
		baseCancel:  baseCancel,
	}
}

// Subscribe creates a new event subscription channel. Each subscriber gets its
// own buffered channel so events are not stolen between controllers. The channel
// is closed when the pool shuts down.
func (wp *WorkerPool) Subscribe() <-chan []RequesterInfo {
	wp.subscribersMu.Lock()
	defer wp.subscribersMu.Unlock()

	ch := make(chan []RequesterInfo, wp.SubscriberBufferSize)
	wp.subscribers = append(wp.subscribers, ch)

	return ch
}

// Start begins the worker pool. It blocks until ctx is canceled to implement
// graceful shutdown, then cancels all in-flight transfers and drains the
// workers within ShutdownTimeout.
func (wp *WorkerPool) Start(ctx context.Context) error {
	wp.Logger.Info("starting transfer worker pool", "workers", wp.WorkerCount, "queueSize", wp.QueueSize, "subscriberBufferSize", wp.SubscriberBufferSize)

	for i := range wp.WorkerCount {
		wp.workersDone.Add(1)
		go wp.worker(ctx, i)
	}

	<-ctx.Done()
	wp.Logger.Info("transfer worker pool shutting down, canceling in-flight transfers")

	// Reject new submissions before tearing anything down. Taking mu serializes
	// this against an in-flight Submit, which holds mu across its send: once
	// closed is set, no further send can start. The workQueue is deliberately
	// never closed, so the `send` can never race a close.
	wp.mu.Lock()
	wp.closed = true
	wp.mu.Unlock()

	// Cancel every per-key context so running transfers stop at the next safe point.
	wp.baseCancel()

	done := make(chan struct{})
	go func() {
		wp.workersDone.Wait()

		wp.subscribersMu.Lock()
		for _, ch := range wp.subscribers {
			close(ch)
		}
		wp.subscribersMu.Unlock()

		close(done)
	}()

	timeout := time.NewTimer(wp.ShutdownTimeout)
	defer timeout.Stop()

	select {
	case <-done:
		wp.Logger.Info("transfer worker pool shutdown complete")

		return nil
	case <-timeout.C:
		return fmt.Errorf("timed out waiting for transfer worker pool to shutdown")
	}
}

// Submit enqueues a transfer for asynchronous execution.
//
// If a transfer for the same key is already queued or running, the requester is
// added to its notification set and [ErrTransferInProgress] is returned without
// re-submitting. On a successful first enqueue it also returns
// [ErrTransferInProgress]. [ErrQueueFull] is returned when the queue is full.
//
// The caller must consume any pending terminal result via [WorkerPool.Result]
// before submitting a new transfer for the same key.
//
// A duplicate submission does not change the in-flight item's Stamp: the result
// carries the Stamp of the transfer that actually ran. If the desired state
// moved on while the transfer was in flight, the caller sees the older Stamp on
// the result and discards it, then re-submits for the new state.
func (wp *WorkerPool) Submit(opts SubmitOptions) error {
	if opts.Key == "" {
		return fmt.Errorf("submit requires a non-empty key")
	}
	if opts.TGD == nil {
		return fmt.Errorf("submit requires a transformation graph definition for key %s", opts.Key)
	}
	if opts.Builder == nil {
		return fmt.Errorf("submit requires a graph builder for key %s", opts.Key)
	}

	wp.mu.Lock()
	defer wp.mu.Unlock()

	if wp.closed {
		return ErrPoolShuttingDown
	}

	if inf, exists := wp.inProgress[opts.Key]; exists {
		inf.addRequester(opts.Requester)
		wp.Logger.V(1).Info("transfer already in progress, added requester", "key", opts.Key, "requester", opts.Requester.NamespacedName)

		return ErrTransferInProgress
	}

	itemCtx, cancel := context.WithCancel(wp.baseCtx)
	item := &workItem{
		key:     opts.Key,
		stamp:   opts.Stamp,
		tgd:     opts.TGD,
		builder: opts.Builder,
		ctx:     itemCtx,
	}

	select {
	case wp.workQueue <- item:
		wp.inProgress[opts.Key] = &inflight{
			requesters: []RequesterInfo{opts.Requester},
			cancel:     cancel,
		}
		TransferInProgressGauge.Set(float64(len(wp.inProgress)))
		TransferQueueSizeGauge.Set(float64(len(wp.workQueue)))
		wp.Logger.V(1).Info("enqueued transfer", "key", opts.Key, "requester", opts.Requester.NamespacedName)

		return ErrTransferInProgress
	default:
		cancel()

		return ErrQueueFull
	}
}

// Result consumes the terminal result for a key, returning it only when its
// Stamp matches desiredStamp.
//
// The caller passes the content identity it currently wants (derived from the
// live source/target/config this reconcile). A result whose Stamp differs is
// stale: the desired state moved on while the transfer was in flight. It is
// dropped and (nil, false) is returned, exactly as if no result were present,
// so the caller re-plans for the current state. A matching result is delivered
// exactly once.
//
// Pass an empty desiredStamp to opt out of staleness checking; it matches a
// result submitted with an empty Stamp.
func (wp *WorkerPool) Result(key, desiredStamp string) (*Result, bool) {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	res, ok := wp.results[key]
	if !ok {
		return nil, false
	}

	delete(wp.results, key)

	if res.Stamp != desiredStamp {
		wp.Logger.V(1).Info("discarded stale transfer result", "key", key, "resultStamp", res.Stamp, "desiredStamp", desiredStamp)

		return nil, false
	}

	return res, true
}

// IsInProgress reports whether a transfer for the key is queued or running. It
// lets the reconciler detect a stale TransferInProgress condition after a pod
// crash or leader change.
func (wp *WorkerPool) IsInProgress(key string) bool {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	_, ok := wp.inProgress[key]

	return ok
}

// Cancel cancels the per-key context of a queued or running transfer. It is a
// no-op if no transfer for the key is in flight. The canceled transfer still
// produces a terminal result and a completion event.
func (wp *WorkerPool) Cancel(key string) {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if inf, ok := wp.inProgress[key]; ok {
		wp.Logger.V(1).Info("canceling in-flight transfer", "key", key)
		inf.cancel()
	}
}

// addRequester appends a requester to the inflight set, deduplicating by name.
func (inf *inflight) addRequester(r RequesterInfo) {
	for _, existing := range inf.requesters {
		if existing.NamespacedName == r.NamespacedName {
			return
		}
	}

	inf.requesters = append(inf.requesters, r)
}

func (wp *WorkerPool) worker(ctx context.Context, id int) {
	defer wp.workersDone.Done()
	logger := wp.Logger.WithValues("worker", id)
	defer logger.V(1).Info("transfer worker stopped")

	for {
		select {
		case <-ctx.Done():
			logger.V(1).Info("transfer worker stopped due to context cancellation")

			return
		case item := <-wp.workQueue:
			TransferQueueSizeGauge.Set(float64(len(wp.workQueue)))
			wp.handleWorkItem(&logger, item)
		}
	}
}

func (wp *WorkerPool) handleWorkItem(logger *logr.Logger, item *workItem) {
	logger.V(1).Info("processing transfer", "key", item.key)

	start := time.Now()
	err := runTransfer(item)
	duration := time.Since(start).Seconds()

	result := resultLabel(err)
	TransferDurationHistogram.WithLabelValues(result).Observe(duration)
	TransferTotal.WithLabelValues(result).Inc()

	if err != nil {
		logger.Error(err, "transfer failed", "key", item.key, "duration", duration)
	} else {
		logger.V(1).Info("transfer complete", "key", item.key, "duration", duration)
	}

	requesters := wp.setResult(item.key, &Result{Stamp: item.stamp, Error: err})
	wp.broadcast(logger, requesters)
}

// runTransfer builds the executable graph from the TGD and processes it. The
// per-key context aborts the transfer on cancellation or pool shutdown.
func runTransfer(item *workItem) error {
	if err := item.ctx.Err(); err != nil {
		return err
	}

	graph, err := item.builder.BuildAndCheck(item.tgd)
	if err != nil {
		return fmt.Errorf("building transformation graph: %w", err)
	}

	if err := graph.Process(item.ctx); err != nil {
		return fmt.Errorf("processing transformation graph: %w", err)
	}

	return nil
}

// setResult stores the terminal result, releases the per-key context, and
// returns the requesters captured at completion time.
func (wp *WorkerPool) setResult(key string, res *Result) []RequesterInfo {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	var requesters []RequesterInfo
	if inf, ok := wp.inProgress[key]; ok {
		requesters = slices.Clone(inf.requesters)
		inf.cancel()
		delete(wp.inProgress, key)
	}

	wp.results[key] = res
	TransferInProgressGauge.Set(float64(len(wp.inProgress)))

	return requesters
}

// broadcast sends the requesters to every subscriber without blocking. Events
// are dropped if a subscriber buffer is full; the periodic reconcile interval
// recovers any dropped completion.
func (wp *WorkerPool) broadcast(logger *logr.Logger, requesters []RequesterInfo) {
	if len(requesters) == 0 {
		return
	}

	wp.subscribersMu.RLock()
	subscribers := slices.Clone(wp.subscribers)
	wp.subscribersMu.RUnlock()

	for _, ch := range subscribers {
		select {
		case ch <- requesters:
		default:
			logger.Info("dropped transfer event, subscriber buffer full", "requesterCount", len(requesters))
			TransferEventChannelDropsTotal.WithLabelValues().Inc()
		}
	}
}

func resultLabel(err error) string {
	switch {
	case err == nil:
		return resultSuccess
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return resultCanceled
	default:
		return resultError
	}
}
