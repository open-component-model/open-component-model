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
	"sigs.k8s.io/controller-runtime/pkg/event"

	"ocm.software/open-component-model/kubernetes/controller/internal/replication/workerpool"
)

// countingTransfer returns a transfer function that counts its runs and then
// delegates to next, succeeding immediately when next is nil.
func countingTransfer(runs *atomic.Int32, next func(ctx context.Context) error) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		runs.Add(1)
		if next != nil {
			return next(ctx)
		}

		return nil
	}
}

// blockingTransfer returns a transfer function that blocks until release is
// closed or the transfer context is canceled, so shutdown is never wedged.
func blockingTransfer(release <-chan struct{}) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
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

func submitOpts(key, requesterName string, transfer func(ctx context.Context) error) workerpool.SubmitOptions {
	return submitOptsStamped(key, "stamp-"+key, requesterName, transfer)
}

func submitOptsStamped(key, stamp, requesterName string, transfer func(ctx context.Context) error) workerpool.SubmitOptions {
	return workerpool.SubmitOptions{
		Key:        key,
		Stamp:      stamp,
		Requester:  requester(requesterName),
		TransferFn: transfer,
	}
}

// waitEvent receives one completion event and returns the requester it carries.
func waitEvent(t *testing.T, ch <-chan event.GenericEvent) types.NamespacedName {
	t.Helper()
	select {
	case evt := <-ch:
		return types.NamespacedName{Namespace: evt.Object.GetNamespace(), Name: evt.Object.GetName()}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for transfer event")

		return types.NamespacedName{}
	}
}

func TestSubmitRunsTransferAndEmitsEvent(t *testing.T) {
	pool := newTestPool(t, workerpool.PoolOptions{MaxConcurrentTransfers: 2})
	events := pool.Events()
	startPool(t, pool)

	var runs atomic.Int32
	err := pool.Submit(submitOpts("uid-1", "repl-1", countingTransfer(&runs, nil)))
	require.ErrorIs(t, err, workerpool.ErrTransferInProgress)

	got := waitEvent(t, events)
	require.Equal(t, requester("repl-1").NamespacedName, got)

	res, ok := pool.Result("uid-1", "stamp-uid-1")
	require.True(t, ok)
	require.NoError(t, res.Error)
	assert.Equal(t, int32(1), runs.Load())

	// Result is delivered exactly once.
	_, ok = pool.Result("uid-1", "stamp-uid-1")
	assert.False(t, ok)
	assert.False(t, pool.IsInProgress("uid-1"))
}

func TestResultCarriesStamp(t *testing.T) {
	pool := newTestPool(t, workerpool.PoolOptions{MaxConcurrentTransfers: 1})
	events := pool.Events()
	startPool(t, pool)

	require.ErrorIs(t, pool.Submit(submitOptsStamped("uid-1", "digest-A", "repl-1", countingTransfer(&atomic.Int32{}, nil))), workerpool.ErrTransferInProgress)

	waitEvent(t, events)
	res, ok := pool.Result("uid-1", "digest-A")
	require.True(t, ok)
	require.NoError(t, res.Error)
	assert.Equal(t, "digest-A", res.Stamp)
}

func TestStaleResultDroppedOnStampMismatch(t *testing.T) {
	// A second submit for the same key while the first is in flight does not
	// change the in-flight stamp. The transfer runs for digest-A, but the
	// consumer has moved on to digest-B: Result must reject and drop the stale
	// outcome.
	pool := newTestPool(t, workerpool.PoolOptions{MaxConcurrentTransfers: 1})
	events := pool.Events()
	startPool(t, pool)

	release := make(chan struct{})
	var runs atomic.Int32
	transfer := countingTransfer(&runs, blockingTransfer(release))

	require.ErrorIs(t, pool.Submit(submitOptsStamped("uid-1", "digest-A", "repl-1", transfer)), workerpool.ErrTransferInProgress)
	require.ErrorIs(t, pool.Submit(submitOptsStamped("uid-1", "digest-B", "repl-1", transfer)), workerpool.ErrTransferInProgress)

	close(release)

	waitEvent(t, events)
	assert.Equal(t, int32(1), runs.Load(), "the deduplicated key must run exactly one transfer")

	_, ok := pool.Result("uid-1", "digest-B")
	require.False(t, ok, "a result for digest-A must not satisfy a consumer that now wants digest-B")

	// The stale result was dropped, not merely hidden: it is gone for good.
	_, ok = pool.Result("uid-1", "digest-A")
	require.False(t, ok)
}

func TestSubmitFailureSurfacesError(t *testing.T) {
	pool := newTestPool(t, workerpool.PoolOptions{MaxConcurrentTransfers: 1})
	events := pool.Events()
	startPool(t, pool)

	wantErr := errors.New("registry unreachable")
	require.ErrorIs(t, pool.Submit(submitOpts("uid-1", "repl-1", func(context.Context) error {
		return wantErr
	})), workerpool.ErrTransferInProgress)

	waitEvent(t, events)

	res, ok := pool.Result("uid-1", "stamp-uid-1")
	require.True(t, ok)
	require.ErrorIs(t, res.Error, wantErr)
}

func TestBurstSubmitDeduplicatesAndCollectsRequesters(t *testing.T) {
	// The first submit blocks in the transfer, so the following submits land
	// while the key is in flight and collapse onto it, adding their requesters.
	pool := newTestPool(t, workerpool.PoolOptions{MaxConcurrentTransfers: 1})
	events := pool.Events()
	startPool(t, pool)

	release := make(chan struct{})
	var runs atomic.Int32
	transfer := countingTransfer(&runs, blockingTransfer(release))

	require.ErrorIs(t, pool.Submit(submitOpts("uid-1", "repl-a", transfer)), workerpool.ErrTransferInProgress)
	require.ErrorIs(t, pool.Submit(submitOpts("uid-1", "repl-b", transfer)), workerpool.ErrTransferInProgress)
	// Same requester again must not duplicate.
	require.ErrorIs(t, pool.Submit(submitOpts("uid-1", "repl-a", transfer)), workerpool.ErrTransferInProgress)

	close(release)

	got := []types.NamespacedName{waitEvent(t, events), waitEvent(t, events)}
	require.ElementsMatch(t, []types.NamespacedName{
		requester("repl-a").NamespacedName,
		requester("repl-b").NamespacedName,
	}, got)
	assert.Equal(t, int32(1), runs.Load(), "transfer must run exactly once for a deduplicated key")
}

func TestConcurrencyCapQueuesExcessTransfers(t *testing.T) {
	// One slot: the second key must wait for the semaphore until the first
	// transfer finishes, then run.
	pool := newTestPool(t, workerpool.PoolOptions{MaxConcurrentTransfers: 1})
	events := pool.Events()
	startPool(t, pool)

	release := make(chan struct{})
	started := make(chan struct{})
	first := func(ctx context.Context) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	var secondRuns atomic.Int32

	require.ErrorIs(t, pool.Submit(submitOpts("uid-1", "repl-1", first)), workerpool.ErrTransferInProgress)
	<-started
	require.ErrorIs(t, pool.Submit(submitOpts("uid-2", "repl-2", countingTransfer(&secondRuns, nil))), workerpool.ErrTransferInProgress)

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int32(0), secondRuns.Load(), "second transfer must wait for a free slot")
	assert.True(t, pool.IsInProgress("uid-2"))

	close(release)

	got := []types.NamespacedName{waitEvent(t, events), waitEvent(t, events)}
	require.ElementsMatch(t, []types.NamespacedName{
		requester("repl-1").NamespacedName,
		requester("repl-2").NamespacedName,
	}, got)
	assert.Equal(t, int32(1), secondRuns.Load())
}

func TestSubmitValidation(t *testing.T) {
	pool := newTestPool(t, workerpool.PoolOptions{MaxConcurrentTransfers: 1})

	err := pool.Submit(workerpool.SubmitOptions{TransferFn: func(context.Context) error { return nil }})
	require.ErrorContains(t, err, "non-empty key")

	err = pool.Submit(workerpool.SubmitOptions{Key: "uid-1"})
	require.ErrorContains(t, err, "transfer function")
}

func TestCancelRunningTransfer(t *testing.T) {
	pool := newTestPool(t, workerpool.PoolOptions{MaxConcurrentTransfers: 1})
	events := pool.Events()
	startPool(t, pool)

	started := make(chan struct{})
	transfer := func(ctx context.Context) error {
		close(started)
		<-ctx.Done()

		return ctx.Err()
	}

	require.ErrorIs(t, pool.Submit(submitOpts("uid-1", "repl-1", transfer)), workerpool.ErrTransferInProgress)

	<-started
	require.True(t, pool.IsInProgress("uid-1"))

	pool.Cancel("uid-1")

	waitEvent(t, events)
	res, ok := pool.Result("uid-1", "stamp-uid-1")
	require.True(t, ok)
	require.ErrorIs(t, res.Error, context.Canceled)
}

func TestCancelQueuedTransfer(t *testing.T) {
	// One slot held by a blocked transfer: the second key waits on the
	// semaphore. Cancel must abort it there, without ever running.
	pool := newTestPool(t, workerpool.PoolOptions{MaxConcurrentTransfers: 1})
	events := pool.Events()
	startPool(t, pool)

	release := make(chan struct{})
	started := make(chan struct{})
	first := func(ctx context.Context) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	var secondRuns atomic.Int32

	require.ErrorIs(t, pool.Submit(submitOpts("uid-1", "repl-1", first)), workerpool.ErrTransferInProgress)
	<-started
	require.ErrorIs(t, pool.Submit(submitOpts("uid-2", "repl-2", countingTransfer(&secondRuns, nil))), workerpool.ErrTransferInProgress)

	pool.Cancel("uid-2")

	got := waitEvent(t, events)
	require.Equal(t, requester("repl-2").NamespacedName, got)
	res, ok := pool.Result("uid-2", "stamp-uid-2")
	require.True(t, ok)
	require.ErrorIs(t, res.Error, context.Canceled)
	assert.Equal(t, int32(0), secondRuns.Load(), "canceled queued transfer must not run")

	close(release)
	waitEvent(t, events)
}

func TestCancelUnknownKeyIsNoop(t *testing.T) {
	pool := newTestPool(t, workerpool.PoolOptions{MaxConcurrentTransfers: 1})
	require.NotPanics(t, func() { pool.Cancel("does-not-exist") })
}

func TestShutdownCancelsInFlightTransfers(t *testing.T) {
	pool := newTestPool(t, workerpool.PoolOptions{MaxConcurrentTransfers: 1})
	events := pool.Events()

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		_ = pool.Start(ctx)
	}()

	started := make(chan struct{})
	transfer := func(ctx context.Context) error {
		close(started)
		<-ctx.Done()

		return ctx.Err()
	}
	require.ErrorIs(t, pool.Submit(submitOpts("uid-1", "repl-1", transfer)), workerpool.ErrTransferInProgress)
	<-started

	cancel()

	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("pool did not stop after context cancellation")
	}

	// The in-flight transfer terminated with a canceled result and its
	// completion event was still emitted.
	waitEvent(t, events)
	res, ok := pool.Result("uid-1", "stamp-uid-1")
	require.True(t, ok)
	require.ErrorIs(t, res.Error, context.Canceled)
}

func TestSubmitAfterShutdownIsRejected(t *testing.T) {
	pool := newTestPool(t, workerpool.PoolOptions{MaxConcurrentTransfers: 1})

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		_ = pool.Start(ctx)
	}()

	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("pool did not stop after context cancellation")
	}

	require.ErrorIs(t, pool.Submit(submitOpts("uid-1", "repl-1", countingTransfer(&atomic.Int32{}, nil))), workerpool.ErrPoolShuttingDown)
}

func TestSubmitAfterResultConsumedReRuns(t *testing.T) {
	pool := newTestPool(t, workerpool.PoolOptions{MaxConcurrentTransfers: 1})
	events := pool.Events()
	startPool(t, pool)

	var runs atomic.Int32
	transfer := countingTransfer(&runs, nil)
	require.ErrorIs(t, pool.Submit(submitOpts("uid-1", "repl-1", transfer)), workerpool.ErrTransferInProgress)
	waitEvent(t, events)
	res, ok := pool.Result("uid-1", "stamp-uid-1")
	require.True(t, ok)
	require.NoError(t, res.Error)

	// A fresh source digest re-submits the same key after the result was consumed.
	require.ErrorIs(t, pool.Submit(submitOpts("uid-1", "repl-1", transfer)), workerpool.ErrTransferInProgress)
	waitEvent(t, events)
	res, ok = pool.Result("uid-1", "stamp-uid-1")
	require.True(t, ok)
	require.NoError(t, res.Error)
	assert.Equal(t, int32(2), runs.Load())
}
