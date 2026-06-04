package workerpool

import (
	"context"

	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// EventSource is a controller-runtime source that subscribes to the worker
// pool's completion events and enqueues reconciliation for every requester when
// a transfer finishes.
type EventSource struct {
	eventChan <-chan []RequesterInfo
}

var _ source.Source = &EventSource{}

// NewEventSource creates a new event source backed by a dedicated subscription
// to the worker pool. Events are broadcast to all subscribers.
func NewEventSource(workerPool *WorkerPool) *EventSource {
	return &EventSource{
		eventChan: workerPool.Subscribe(),
	}
}

// Start implements source.Source. It watches the event channel and enqueues a
// reconciliation request for every requester when transfers complete.
func (es *EventSource) Start(ctx context.Context, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) error {
	logger := ctrl.LoggerFrom(ctx).WithName("transfer-event-source")

	go func() {
		for {
			select {
			case <-ctx.Done():
				logger.Info("stopping transfer event source due to context cancellation")

				return
			case requesters, ok := <-es.eventChan:
				if !ok {
					logger.Info("event channel closed, stopping transfer event source")

					return
				}

				for _, requester := range requesters {
					queue.Add(reconcile.Request{NamespacedName: requester.NamespacedName})
					logger.V(1).Info("enqueued reconciliation request from transfer event", "requester", requester.NamespacedName)
				}
			}
		}
	}()

	return nil
}

// String implements source.Source.
func (es *EventSource) String() string {
	return "transfer-event-source"
}
