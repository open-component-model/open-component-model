package workerpool

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/sync/semaphore"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/event"

	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

// ErrTransferInProgress is returned by [WorkerPool.Submit] when a transfer for
// the given key has been accepted and is queued or running. The reconciler
// treats it as a signal to exit and wait for the completion event.
var ErrTransferInProgress = errors.New("component version transfer in progress")

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
	// MaxConcurrentTransfers caps how many transfers run at the same time.
	MaxConcurrentTransfers int
	// EventBufferSize is the buffer size of the completion event channel. A
	// larger buffer reduces the probability of dropped events under load.
	EventBufferSize int
	// ShutdownTimeout bounds how long Start waits for in-flight transfers to
	// drain on shutdown. Defaults to 5s.
	ShutdownTimeout time.Duration
	// Logger for the worker pool. Defaults to a no-op logger.
	Logger *logr.Logger
}

// inflight tracks the state of a queued or running transfer.
type inflight struct {
	// requesters are all objects waiting on this key, notified on completion.
	requesters []RequesterInfo
	// cancel cancels the per-key context, used for the deletion drain and on
	// pool shutdown. A transfer still waiting for a semaphore slot aborts
	// immediately: Acquire returns the context error.
	cancel context.CancelFunc
}

// workItem is the internal unit processed by a transfer goroutine.
type workItem struct {
	key     string
	stamp   string
	tgd     *transformv1alpha1.TransformationGraphDefinition
	builder GraphBuilder
	// ctx is the per-key context, derived from the pool base context.
	ctx context.Context
}

// WorkerPool runs component version transfers asynchronously. Submission is
// non-blocking: each accepted transfer runs in its own goroutine gated by a
// concurrency semaphore. There is no work queue; in-flight work is bounded by
// the per-key dedup, one item per Replication CR. Completions are emitted as
// generic events on a single channel, consumed by the controller via
// source.Channel.
type WorkerPool struct {
	PoolOptions

	// sem caps the number of concurrently running transfers per pool.
	sem *semaphore.Weighted

	// events carries one event per requester on transfer completion. It is
	// never closed; sends are non-blocking and drop when the buffer is full,
	// the periodic reconcile interval recovers a dropped completion.
	events chan event.GenericEvent

	// mu guards inProgress, results, and closed.
	mu         sync.Mutex
	inProgress map[string]*inflight
	results    map[string]*Result
	// closed is set when shutdown begins. It gates Submit so no transfer is
	// started after the drain has begun.
	closed bool

	// baseCtx is the parent of every per-key context. It is canceled on
	// shutdown so all in-flight transfers stop.
	baseCtx    context.Context
	baseCancel context.CancelFunc

	transfersDone sync.WaitGroup
}

const (
	defaultMaxConcurrentTransfersPerPool = 5
	defaultEventBufferSize               = 100
	defaultShutdownTimeout               = 5 * time.Second
)

// NewWorkerPool creates a new transfer worker pool. The pool accepts and runs
// transfers from construction; Start only implements graceful shutdown.
func NewWorkerPool(opts PoolOptions) *WorkerPool {
	if opts.MaxConcurrentTransfers <= 0 {
		opts.MaxConcurrentTransfers = defaultMaxConcurrentTransfersPerPool
	}

	if opts.EventBufferSize <= 0 {
		opts.EventBufferSize = defaultEventBufferSize
	}

	if opts.ShutdownTimeout <= 0 {
		opts.ShutdownTimeout = defaultShutdownTimeout
	}

	if opts.Logger == nil {
		opts.Logger = new(logr.Discard())
	}

	baseCtx, baseCancel := context.WithCancel(context.Background()) //nolint:gosec // it's called later on in Start.

	return &WorkerPool{
		PoolOptions: opts,
		sem:         semaphore.NewWeighted(int64(opts.MaxConcurrentTransfers)),
		events:      make(chan event.GenericEvent, opts.EventBufferSize),
		inProgress:  make(map[string]*inflight),
		results:     make(map[string]*Result),
		baseCtx:     baseCtx,
		baseCancel:  baseCancel,
	}
}

// Events returns the completion event channel. Wire it into the controller
// with source.Channel(pool.Events(), &handler.EnqueueRequestForObject{}).
func (wp *WorkerPool) Events() <-chan event.GenericEvent {
	return wp.events
}

// Start implements manager.Runnable. It blocks until ctx is canceled, then
// rejects new submissions, cancels all in-flight transfers, and waits for them
// to drain within ShutdownTimeout.
func (wp *WorkerPool) Start(ctx context.Context) error {
	wp.Logger.Info("transfer worker pool started", "maxConcurrentTransfers", wp.MaxConcurrentTransfers, "eventBufferSize", wp.EventBufferSize)

	<-ctx.Done()
	wp.Logger.Info("transfer worker pool shutting down, canceling in-flight transfers")

	// Reject new submissions before tearing anything down. Taking mu serializes
	// this against an in-flight Submit: once closed is set, no further transfer
	// goroutine can start.
	wp.mu.Lock()
	wp.closed = true
	wp.mu.Unlock()

	// Cancel every per-key context so running transfers stop at the next safe
	// point and queued ones abort. This is the `baseContext`. Ever new context
	// gets this context as a parent, so using Go's Context cancel propagation
	// to close all of them out.
	wp.baseCancel()

	done := make(chan struct{})
	go func() {
		// wait for all in-progress transfers to cancel out.
		wp.transfersDone.Wait()
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

// Submit accepts a transfer for asynchronous execution.
//
// If a transfer for the same key is already queued or running, the requester is
// added to its notification set and [ErrTransferInProgress] is returned without
// re-submitting. On a successful first submission it also returns
// [ErrTransferInProgress].
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

	itemCtx, cancel := context.WithCancel(wp.baseCtx) //nolint:gosec // the cancel is called optionally from inflight
	wp.inProgress[opts.Key] = &inflight{
		requesters: []RequesterInfo{opts.Requester},
		cancel:     cancel,
	}
	TransferInProgressGauge.Set(float64(len(wp.inProgress)))
	wp.Logger.V(1).Info("accepted transfer", "key", opts.Key, "requester", opts.Requester.NamespacedName)

	wp.transfersDone.Add(1)
	go wp.run(&workItem{
		key:     opts.Key,
		stamp:   opts.Stamp,
		tgd:     opts.TGD,
		builder: opts.Builder,
		ctx:     itemCtx,
	})

	return ErrTransferInProgress
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

// run executes a single transfer: it waits for a semaphore slot, runs the
// graph, records the terminal result, and emits the completion events.
func (wp *WorkerPool) run(item *workItem) {
	defer wp.transfersDone.Done()

	err := wp.sem.Acquire(item.ctx, 1)
	if err == nil {
		start := time.Now()
		err = runTransfer(item)
		wp.sem.Release(1)
		TransferDurationHistogram.WithLabelValues(resultLabel(err)).Observe(time.Since(start).Seconds())
	}

	TransferTotal.WithLabelValues(resultLabel(err)).Inc()

	if err != nil {
		wp.Logger.Error(err, "transfer failed", "key", item.key)
	} else {
		wp.Logger.V(1).Info("transfer complete", "key", item.key)
	}

	requesters := wp.setResult(item.key, &Result{Stamp: item.stamp, Error: err})
	wp.emit(requesters)
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

// emit sends one completion event per requester without blocking. Events are
// dropped if the buffer is full. A periodic reconcile interval can be used as
// a fallback mechanism.
func (wp *WorkerPool) emit(requesters []RequesterInfo) {
	for _, r := range requesters {
		evt := event.GenericEvent{Object: &metav1.PartialObjectMetadata{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: r.NamespacedName.Namespace,
				Name:      r.NamespacedName.Name,
			},
		}}

		select {
		case wp.events <- evt:
		default:
			wp.Logger.Info("dropped transfer completion event, buffer full", "requester", r.NamespacedName)
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
