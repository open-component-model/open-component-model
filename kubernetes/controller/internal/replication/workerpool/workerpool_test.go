package workerpool_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/replication/workerpool"
)

// fakeBuilder is a test GraphBuilder. buildErr fails the build step; process, if
// set, runs as the graph execution and otherwise succeeds immediately.
type fakeBuilder struct {
	buildErr error
	process  func(ctx context.Context) error
	builds   atomic.Int32
}

func (f *fakeBuilder) BuildAndCheck(_ *transformv1alpha1.TransformationGraphDefinition) (workerpool.Graph, error) {
	f.builds.Add(1)
	if f.buildErr != nil {
		return nil, f.buildErr
	}

	return fakeGraph{process: f.process}, nil
}

type fakeGraph struct {
	process func(ctx context.Context) error
}

func (g fakeGraph) Process(ctx context.Context) error {
	if g.process != nil {
		return g.process(ctx)
	}

	return nil
}

func newTestPool(t *testing.T, opts workerpool.PoolOptions) *workerpool.WorkerPool {
	t.Helper()
	logger := logr.Discard()
	opts.Logger = &logger

	return workerpool.NewWorkerPool(opts)
}

// startPool runs the pool until the test ends.
func startPool(t *testing.T, pool *workerpool.WorkerPool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = pool.Start(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("pool did not shut down in time")
		}
	})
}

func requester(name string) workerpool.RequesterInfo {
	return workerpool.RequesterInfo{NamespacedName: types.NamespacedName{Namespace: "default", Name: name}}
}

func submitOpts(key, requesterName string, builder workerpool.GraphBuilder) workerpool.SubmitOptions {
	return submitOptsStamped(key, "stamp-"+key, requesterName, builder)
}

func submitOptsStamped(key, stamp, requesterName string, builder workerpool.GraphBuilder) workerpool.SubmitOptions {
	return workerpool.SubmitOptions{
		Key:       key,
		Stamp:     stamp,
		Requester: requester(requesterName),
		TGD:       &transformv1alpha1.TransformationGraphDefinition{},
		Builder:   builder,
	}
}

func waitEvent(t *testing.T, ch <-chan []workerpool.RequesterInfo) []workerpool.RequesterInfo {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for transfer event")

		return nil
	}
}

func TestSubmitRunsTransferAndBroadcasts(t *testing.T) {
	pool := newTestPool(t, workerpool.PoolOptions{WorkerCount: 2, QueueSize: 8})
	events := pool.Subscribe()
	startPool(t, pool)

	builder := &fakeBuilder{}
	err := pool.Submit(submitOpts("uid-1", "repl-1", builder))
	require.ErrorIs(t, err, workerpool.ErrTransferInProgress)

	got := waitEvent(t, events)
	require.Equal(t, []workerpool.RequesterInfo{requester("repl-1")}, got)

	res, ok := pool.Result("uid-1", "stamp-uid-1")
	require.True(t, ok)
	require.NoError(t, res.Error)
	assert.Equal(t, int32(1), builder.builds.Load())

	// Result is delivered exactly once.
	_, ok = pool.Result("uid-1", "stamp-uid-1")
	assert.False(t, ok)
	assert.False(t, pool.IsInProgress("uid-1"))
}

func TestResultCarriesStamp(t *testing.T) {
	pool := newTestPool(t, workerpool.PoolOptions{WorkerCount: 1, QueueSize: 4})
	events := pool.Subscribe()
	startPool(t, pool)

	require.ErrorIs(t, pool.Submit(submitOptsStamped("uid-1", "digest-A", "repl-1", &fakeBuilder{})), workerpool.ErrTransferInProgress)

	waitEvent(t, events)
	res, ok := pool.Result("uid-1", "digest-A")
	require.True(t, ok)
	require.NoError(t, res.Error)
	assert.Equal(t, "digest-A", res.Stamp)
}

func TestStaleResultDroppedOnStampMismatch(t *testing.T) {
	// A second submit for the same key while the first is queued does not change
	// the in-flight stamp. The transfer runs for digest-A, but the consumer has
	// moved on to digest-B: Result must reject and drop the stale outcome.
	pool := newTestPool(t, workerpool.PoolOptions{WorkerCount: 1, QueueSize: 4})
	events := pool.Subscribe()

	builder := &fakeBuilder{}
	require.ErrorIs(t, pool.Submit(submitOptsStamped("uid-1", "digest-A", "repl-1", builder)), workerpool.ErrTransferInProgress)
	require.ErrorIs(t, pool.Submit(submitOptsStamped("uid-1", "digest-B", "repl-1", builder)), workerpool.ErrTransferInProgress)

	startPool(t, pool)

	waitEvent(t, events)
	assert.Equal(t, int32(1), builder.builds.Load(), "the deduplicated key must run exactly one transfer")

	_, ok := pool.Result("uid-1", "digest-B")
	require.False(t, ok, "a result for digest-A must not satisfy a consumer that now wants digest-B")

	// The stale result was dropped, not merely hidden: it is gone for good.
	_, ok = pool.Result("uid-1", "digest-A")
	require.False(t, ok)
}

func TestSubmitFailureSurfacesError(t *testing.T) {
	pool := newTestPool(t, workerpool.PoolOptions{WorkerCount: 1, QueueSize: 4})
	events := pool.Subscribe()
	startPool(t, pool)

	wantErr := errors.New("registry unreachable")
	require.ErrorIs(t, pool.Submit(submitOpts("uid-1", "repl-1", &fakeBuilder{process: func(context.Context) error {
		return wantErr
	}})), workerpool.ErrTransferInProgress)

	waitEvent(t, events)

	res, ok := pool.Result("uid-1", "stamp-uid-1")
	require.True(t, ok)
	require.ErrorIs(t, res.Error, wantErr)
}

func TestSubmitBuildFailureSurfacesError(t *testing.T) {
	pool := newTestPool(t, workerpool.PoolOptions{WorkerCount: 1, QueueSize: 4})
	events := pool.Subscribe()
	startPool(t, pool)

	wantErr := errors.New("invalid graph")
	require.ErrorIs(t, pool.Submit(submitOpts("uid-1", "repl-1", &fakeBuilder{buildErr: wantErr})), workerpool.ErrTransferInProgress)

	waitEvent(t, events)

	res, ok := pool.Result("uid-1", "stamp-uid-1")
	require.True(t, ok)
	require.ErrorIs(t, res.Error, wantErr)
}

func TestBurstSubmitDeduplicatesAndCollectsRequesters(t *testing.T) {
	// No workers started yet: both submits land before processing, so the
	// second collapses onto the in-flight key and adds its requester.
	pool := newTestPool(t, workerpool.PoolOptions{WorkerCount: 1, QueueSize: 4})
	events := pool.Subscribe()

	block := make(chan struct{})
	builder := &fakeBuilder{process: func(context.Context) error {
		<-block

		return nil
	}}

	require.ErrorIs(t, pool.Submit(submitOpts("uid-1", "repl-a", builder)), workerpool.ErrTransferInProgress)
	require.ErrorIs(t, pool.Submit(submitOpts("uid-1", "repl-b", builder)), workerpool.ErrTransferInProgress)
	// Same requester again must not duplicate.
	require.ErrorIs(t, pool.Submit(submitOpts("uid-1", "repl-a", builder)), workerpool.ErrTransferInProgress)

	startPool(t, pool)
	close(block)

	got := waitEvent(t, events)
	require.ElementsMatch(t, []workerpool.RequesterInfo{requester("repl-a"), requester("repl-b")}, got)
	assert.Equal(t, int32(1), builder.builds.Load(), "transfer must run exactly once for a deduplicated key")
}

func TestSubmitQueueFull(t *testing.T) {
	// One slot, no workers: the slot fills and the next distinct key overflows.
	pool := newTestPool(t, workerpool.PoolOptions{WorkerCount: 1, QueueSize: 1})

	require.ErrorIs(t, pool.Submit(submitOpts("uid-1", "repl-1", &fakeBuilder{})), workerpool.ErrTransferInProgress)
	require.ErrorIs(t, pool.Submit(submitOpts("uid-2", "repl-2", &fakeBuilder{})), workerpool.ErrQueueFull)
}

func TestSubmitValidation(t *testing.T) {
	pool := newTestPool(t, workerpool.PoolOptions{WorkerCount: 1, QueueSize: 1})

	err := pool.Submit(workerpool.SubmitOptions{TGD: &transformv1alpha1.TransformationGraphDefinition{}, Builder: &fakeBuilder{}})
	require.ErrorContains(t, err, "non-empty key")

	err = pool.Submit(workerpool.SubmitOptions{Key: "uid-1", Builder: &fakeBuilder{}})
	require.ErrorContains(t, err, "transformation graph definition")

	err = pool.Submit(workerpool.SubmitOptions{Key: "uid-1", TGD: &transformv1alpha1.TransformationGraphDefinition{}})
	require.ErrorContains(t, err, "graph builder")
}

func TestCancelRunningTransfer(t *testing.T) {
	pool := newTestPool(t, workerpool.PoolOptions{WorkerCount: 1, QueueSize: 4})
	events := pool.Subscribe()
	startPool(t, pool)

	started := make(chan struct{})
	builder := &fakeBuilder{process: func(ctx context.Context) error {
		close(started)
		<-ctx.Done()

		return ctx.Err()
	}}

	require.ErrorIs(t, pool.Submit(submitOpts("uid-1", "repl-1", builder)), workerpool.ErrTransferInProgress)

	<-started
	require.True(t, pool.IsInProgress("uid-1"))

	pool.Cancel("uid-1")

	waitEvent(t, events)
	res, ok := pool.Result("uid-1", "stamp-uid-1")
	require.True(t, ok)
	require.ErrorIs(t, res.Error, context.Canceled)
}

func TestCancelQueuedTransfer(t *testing.T) {
	// Cancel before any worker runs: the worker must short-circuit the item.
	pool := newTestPool(t, workerpool.PoolOptions{WorkerCount: 1, QueueSize: 4})
	events := pool.Subscribe()

	builder := &fakeBuilder{}
	require.ErrorIs(t, pool.Submit(submitOpts("uid-1", "repl-1", builder)), workerpool.ErrTransferInProgress)
	pool.Cancel("uid-1")

	startPool(t, pool)

	waitEvent(t, events)
	res, ok := pool.Result("uid-1", "stamp-uid-1")
	require.True(t, ok)
	require.ErrorIs(t, res.Error, context.Canceled)
	assert.Equal(t, int32(0), builder.builds.Load(), "canceled queued transfer must not build a graph")
}

func TestCancelUnknownKeyIsNoop(t *testing.T) {
	pool := newTestPool(t, workerpool.PoolOptions{WorkerCount: 1, QueueSize: 1})
	require.NotPanics(t, func() { pool.Cancel("does-not-exist") })
}

func TestShutdownCancelsInFlightAndClosesSubscribers(t *testing.T) {
	pool := newTestPool(t, workerpool.PoolOptions{WorkerCount: 1, QueueSize: 4})
	events := pool.Subscribe()

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		_ = pool.Start(ctx)
	}()

	started := make(chan struct{})
	builder := &fakeBuilder{process: func(ctx context.Context) error {
		close(started)
		<-ctx.Done()

		return ctx.Err()
	}}
	require.ErrorIs(t, pool.Submit(submitOpts("uid-1", "repl-1", builder)), workerpool.ErrTransferInProgress)
	<-started

	cancel()

	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("pool did not stop after context cancellation")
	}

	// The subscriber channel is closed on shutdown.
	_, open := <-events
	for open {
		_, open = <-events
	}
}

func TestSubmitAfterResultConsumedReRuns(t *testing.T) {
	pool := newTestPool(t, workerpool.PoolOptions{WorkerCount: 1, QueueSize: 4})
	events := pool.Subscribe()
	startPool(t, pool)

	builder := &fakeBuilder{}
	require.ErrorIs(t, pool.Submit(submitOpts("uid-1", "repl-1", builder)), workerpool.ErrTransferInProgress)
	waitEvent(t, events)
	res, ok := pool.Result("uid-1", "stamp-uid-1")
	require.True(t, ok)
	require.NoError(t, res.Error)

	// A fresh source digest re-submits the same key after the result was consumed.
	require.ErrorIs(t, pool.Submit(submitOpts("uid-1", "repl-1", builder)), workerpool.ErrTransferInProgress)
	waitEvent(t, events)
	res, ok = pool.Result("uid-1", "stamp-uid-1")
	require.True(t, ok)
	require.NoError(t, res.Error)
	assert.Equal(t, int32(2), builder.builds.Load())
}
