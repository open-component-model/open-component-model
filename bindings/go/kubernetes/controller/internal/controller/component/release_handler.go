package component

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

// discoveryReleaseHandler enqueues Components referenced by Discoveries when
// the referred Component is marked for deletion. It is the reverse mapping for
// the Component deletion guard: deleting or retargeting a Discovery must
// release the previously referenced Component. Update events map both the old
// and the new reference so retargeting releases the old Component.
type discoveryReleaseHandler struct {
	client client.Reader
}

var _ handler.EventHandler = (*discoveryReleaseHandler)(nil)

func (h *discoveryReleaseHandler) Create(ctx context.Context, evt event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.enqueueForDeletion(ctx, evt.Object, q)
}

func (h *discoveryReleaseHandler) Update(ctx context.Context, evt event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.enqueueForDeletion(ctx, evt.ObjectOld, q)
	h.enqueueForDeletion(ctx, evt.ObjectNew, q)
}

func (h *discoveryReleaseHandler) Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.enqueueForDeletion(ctx, evt.Object, q)
}

func (h *discoveryReleaseHandler) Generic(ctx context.Context, evt event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	h.enqueueForDeletion(ctx, evt.Object, q)
}

// enqueueForDeletion resolves the Discovery's component reference and enqueues
// the Component only if it is actually marked for deletion.
func (h *discoveryReleaseHandler) enqueueForDeletion(ctx context.Context, obj client.Object, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	discovery, ok := obj.(*v1alpha1.Discovery)
	if !ok || discovery == nil {
		return
	}

	key := types.NamespacedName{
		Namespace: discovery.GetNamespace(),
		Name:      discovery.Spec.ComponentRef.Name,
	}
	component := &v1alpha1.Component{}
	if err := h.client.Get(ctx, key, component); err != nil {
		return
	}
	if component.GetDeletionTimestamp().IsZero() {
		return
	}

	q.Add(reconcile.Request{NamespacedName: key})
}
