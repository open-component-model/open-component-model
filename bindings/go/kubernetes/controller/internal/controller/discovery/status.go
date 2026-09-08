package discovery

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/event"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/status"
)

const (
	// DefaultSafetyInterval is the default controller-wide safety interval
	// for periodic full re-discovery.
	DefaultSafetyInterval = 30 * time.Minute
	// safetyJitter is the relative jitter applied to the safety interval.
	safetyJitter = 0.1
)

// ValidateSafetyInterval validates the controller-wide safety interval. Zero
// disables safety scheduling; negative durations are invalid.
func ValidateSafetyInterval(d time.Duration) error {
	if d < 0 {
		return fmt.Errorf("discovery controller safety interval must not be negative: %s", d)
	}
	return nil
}

// safetyRequeueAfter returns the jittered safety interval for a successful
// reconcile, or 0 when safety scheduling is disabled.
func (r *Reconciler) safetyRequeueAfter() time.Duration {
	if r.SafetyInterval <= 0 {
		return 0
	}
	f := rand.Float64
	if r.randFloat != nil {
		f = r.randFloat
	}
	return jitterDuration(r.SafetyInterval, f())
}

// jitterDuration applies a relative jitter of ±10% to d based on f in
// [0.0, 1.0).
func jitterDuration(d time.Duration, f float64) time.Duration {
	delta := (f*2 - 1) * safetyJitter
	return d + time.Duration(delta*float64(d))
}

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
	r.markStalled(fresh, v1alpha1.PayloadTooLargeReason,
		fmt.Errorf("status payload exceeds the API server size limit; refine the selectors or extraction"))

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

// setCondition sets a condition with the condition-level observed generation
// of the object it was computed for.
func (r *Reconciler) setCondition(discovery *v1alpha1.Discovery, condType string, condStatus metav1.ConditionStatus, reason, msg string) {
	status.SetCondition(discovery, metav1.Condition{
		Type:               condType,
		Status:             condStatus,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: discovery.GetGeneration(),
	})
}

// markFailure marks a retryable failure: Ready=False with the given reason.
// The last successful payload is retained.
func (r *Reconciler) markFailure(discovery *v1alpha1.Discovery, reason string, err error) {
	status.RemoveCondition(discovery, v1alpha1.ReconcilingCondition)
	status.RemoveCondition(discovery, v1alpha1.StalledCondition)
	r.setCondition(discovery, v1alpha1.ReadyCondition, metav1.ConditionFalse, reason, err.Error())
	event.New(r.EventRecorder, discovery, discovery.GetVID(), v1alpha1.EventSeverityError, "%s", err.Error())
}

// markStalled marks a terminal failure requiring a change to recover:
// Ready=False and Stalled=True with the given reason. The last successful
// payload is retained. Terminal failures do not schedule periodic work.
func (r *Reconciler) markStalled(discovery *v1alpha1.Discovery, reason string, err error) {
	status.RemoveCondition(discovery, v1alpha1.ReconcilingCondition)
	r.setCondition(discovery, v1alpha1.ReadyCondition, metav1.ConditionFalse, reason, err.Error())
	r.setCondition(discovery, v1alpha1.StalledCondition, metav1.ConditionTrue, reason, err.Error())
	event.New(r.EventRecorder, discovery, discovery.GetVID(), v1alpha1.EventSeverityError, "%s", err.Error())
}

// markSuccess marks a successful evaluation, including empty results:
// Ready=True with the given reason, Stalled and Reconciling removed, and the
// top-level observed generation advanced.
func (r *Reconciler) markSuccess(discovery *v1alpha1.Discovery, reason, msg string) {
	status.RemoveCondition(discovery, v1alpha1.ReconcilingCondition)
	status.RemoveCondition(discovery, v1alpha1.StalledCondition)
	r.setCondition(discovery, v1alpha1.ReadyCondition, metav1.ConditionTrue, reason, msg)
	discovery.SetObservedGeneration(discovery.GetGeneration())
	event.New(r.EventRecorder, discovery, discovery.GetVID(), v1alpha1.EventSeverityInfo, "%s", msg)
}
