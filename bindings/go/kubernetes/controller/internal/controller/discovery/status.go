package discovery

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/status"
)

// publish writes the mutated status to the API server with an
// optimistic-lock merge patch. Only semantic status changes are published; an
// unchanged status is not written at all. Optimistic-lock conflicts are
// returned retryable so the next reconcile recomputes the status against the
// newer object instead of publishing against a stale spec.
func (r *Reconciler) publish(ctx context.Context, old, discovery *v1alpha1.Discovery) error {
	if equality.Semantic.DeepEqual(old.Status, discovery.Status) {
		return nil
	}
	return r.GetClient().Status().Patch(ctx, discovery,
		client.MergeFromWithOptions(old, client.MergeFromWithOptimisticLock{}))
}

// publishPayloadTooLarge is the fallback for an oversized status candidate
// rejected by the API server. It re-reads the object, confirms the persisted
// generation still matches the attempted computation, and patches only the
// PayloadTooLarge failure conditions. The persisted payload, effective
// config, and observed generation are retained; the oversized candidate is
// never retried. A persistent conflict or other API error is retryable.
func (r *Reconciler) publishPayloadTooLarge(ctx context.Context, discovery *v1alpha1.Discovery) error {
	log.FromContext(ctx).Info("status payload exceeds the API server size limit, falling back to condition-only publication")

	fresh := &v1alpha1.Discovery{}
	if err := r.GetClient().Get(ctx, client.ObjectKeyFromObject(discovery), fresh); err != nil {
		return fmt.Errorf("failed to re-read discovery after oversized status rejection: %w", err)
	}
	if fresh.GetGeneration() != discovery.GetGeneration() {
		// The spec changed concurrently. The generation-changed watch
		// schedules the recomputation against the newer spec.
		return nil
	}

	base := fresh.DeepCopy()
	status.MarkAsStalled(r.EventRecorder, fresh, v1alpha1.PayloadTooLargeReason,
		"status payload exceeds the API server size limit; refine the selectors or extraction")

	if equality.Semantic.DeepEqual(base.Status, fresh.Status) {
		return nil
	}
	if err := r.GetClient().Status().Patch(ctx, fresh,
		client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
		return fmt.Errorf("failed to publish payload-too-large conditions: %w", err)
	}

	return nil
}

// isPayloadTooLarge reports whether err is a real API-server/etcd payload-size
// rejection, as opposed to an unrelated API error. Verified against real
// API-server behavior in envtest: depending on which limit trips, the rejection
// is either a RequestEntityTooLarge status error, an etcd "request too large"
// translation, or a 500 wrapping etcd's gRPC ResourceExhausted rejection
// ("trying to send message larger than max").
func isPayloadTooLarge(err error) bool {
	if err == nil {
		return false
	}
	if apierrors.IsRequestEntityTooLargeError(err) {
		return true
	}
	msg := err.Error()
	if strings.Contains(msg, "ResourceExhausted") && strings.Contains(msg, "larger than max") {
		return true
	}
	return strings.Contains(msg, "request") && strings.Contains(msg, "too large")
}
