package status

import (
	"context"
	"time"

	eventv1 "github.com/fluxcd/pkg/apis/event/v1beta1"
	"github.com/fluxcd/pkg/runtime/patch"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kuberecorder "k8s.io/client-go/tools/record"

	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/event"
)

// UpdateStatus takes an object which can identify itself and updates its status including ObservedGeneration.
func UpdateStatus(
	ctx context.Context,
	patchHelper *patch.SerialPatcher,
	obj IdentifiableClientObject,
	recorder kuberecorder.EventRecorder,
	requeue time.Duration,
	err error,
) error {
	// If still reconciling then reconciliation did not succeed, set to ProgressingWithRetry to
	// indicate that reconciliation will be retried.
	if IsReconciling(obj) && err != nil {
		if reconciling := FindCondition(obj, v1alpha1.ReconcilingCondition); reconciling != nil {
			SetCondition(obj, metav1.Condition{
				Type:    v1alpha1.ReconcilingCondition,
				Status:  metav1.ConditionTrue,
				Reason:  v1alpha1.ProgressingWithRetryReason,
				Message: reconciling.Message,
			})
		}
		event.New(recorder, obj, obj.GetVID(), eventv1.EventSeverityError, "Reconciliation did not succeed, keep retrying")
	}

	// Set status observed generation option if the object is ready.
	if IsReady(obj) {
		obj.SetObservedGeneration(obj.GetGeneration())
		if requeue > 0 {
			event.New(recorder, obj, obj.GetVID(), eventv1.EventSeverityInfo, "Reconciliation finished, next run in %s", requeue)
		} else {
			event.New(recorder, obj, obj.GetVID(), eventv1.EventSeverityInfo, "Reconciliation finished, no further runs scheduled until next change")
		}
	}

	// Update the object.
	return patchHelper.Patch(ctx, obj)
}
