package workerpool

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// EventSource is a controller-runtime source that watches the worker pool's event channel
// and triggers reconciliation for all requesters when a resolution completes.
type EventSource struct {
	workerPool *WorkerPool
}

var _ source.Source = &EventSource{}

// NewEventSource creates a new event source from a worker pool.
func NewEventSource(workerPool *WorkerPool) *EventSource {
	return &EventSource{
		workerPool: workerPool,
	}
}

// Start implements source.Source. It starts watching the event channel and enqueues
// reconciliation requests for all requesters when resolutions complete.
func (es *EventSource) Start(ctx context.Context, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) error {
	logger := ctrl.LoggerFrom(ctx).WithName("resolution-event-source")

	go func() {
		for {
			select {
			case <-ctx.Done():
				logger.Info("stopping resolution event source")
				return
			case event, ok := <-es.workerPool.EventChannel():
				if !ok {
					logger.Info("event channel closed, stopping resolution event source")
					return
				}

				for _, requester := range event.Requesters {
					queue.Add(reconcile.Request{NamespacedName: requester.NamespacedName})
					logger.V(1).Info("enqueued reconciliation request from resolution event",
						"component", event.Component,
						"version", event.Version,
						"requester", requester.NamespacedName,
						"error", event.Error)
				}
			}
		}
	}()

	return nil
}

// String implements source.Source.
func (es *EventSource) String() string {
	return "resolution-event-source"
}

// WaitForSync implements source.SyncingSource (optional).
// The event source is always synced since it only watches a channel.
func (es *EventSource) WaitForSync(ctx context.Context) error {
	return nil
}

var _ handler.EventHandler = &EnqueueRequestForResolution{}

// EnqueueRequestForResolution is a handler.EventHandler that enqueues requests when resolution events occur.
// This is typically not needed since the EventSource directly enqueues requests, but it's provided
// for compatibility with the standard Watch pattern.
type EnqueueRequestForResolution struct{}

// Create implements handler.EventHandler.
func (e *EnqueueRequestForResolution) Create(_ context.Context, _ event.CreateEvent, _ workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	// Not applicable for resolution events
}

// Update implements handler.EventHandler.
func (e *EnqueueRequestForResolution) Update(_ context.Context, _ event.UpdateEvent, _ workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	// Not applicable for resolution events
}

// Delete implements handler.EventHandler.
func (e *EnqueueRequestForResolution) Delete(_ context.Context, _ event.DeleteEvent, _ workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	// Not applicable for resolution events
}

// Generic implements handler.EventHandler.
func (e *EnqueueRequestForResolution) Generic(_ context.Context, evt event.GenericEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	if evt.Object == nil {
		return
	}

	queue.Add(reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: evt.Object.GetNamespace(),
			Name:      evt.Object.GetName(),
		},
	})
}
