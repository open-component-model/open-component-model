package workerpool_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"ocm.software/open-component-model/kubernetes/controller/internal/replication/workerpool"
)

func TestEventSourceEnqueuesRequestersOnCompletion(t *testing.T) {
	pool := newTestPool(t, workerpool.PoolOptions{WorkerCount: 1, QueueSize: 4})

	source := workerpool.NewEventSource(pool)
	assert.Equal(t, "transfer-event-source", source.String())

	startPool(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	t.Cleanup(queue.ShutDown)
	require.NoError(t, source.Start(ctx, queue))

	require.ErrorIs(t, pool.Submit(submitOpts("uid-1", "repl-1", &fakeBuilder{})), workerpool.ErrTransferInProgress)

	require.Eventually(t, func() bool { return queue.Len() == 1 }, 3*time.Second, 5*time.Millisecond)

	req, _ := queue.Get()
	assert.Equal(t, requester("repl-1").NamespacedName, req.NamespacedName)
}
