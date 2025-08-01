package status

import (
	"context"
	"time"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"
	"github.com/fluxcd/pkg/runtime/patch"

	eventv1 "github.com/fluxcd/pkg/apis/event/v1beta1"
	kuberecorder "k8s.io/client-go/tools/record"

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
	// This will add another indicator that we are indeed doing something. This is in addition to
	// the status that is already present on the object which is the Ready condition.
	if conditions.IsReconciling(obj) && err != nil {
		reconciling := conditions.Get(obj, meta.ReconcilingCondition)
		reconciling.Reason = meta.ProgressingWithRetryReason
		conditions.Set(obj, reconciling)
		event.New(recorder, obj, obj.GetVID(), eventv1.EventSeverityError, "Reconciliation did not succeed, keep retrying")
	}

	// Set status observed generation option if the component is ready.
	if conditions.IsReady(obj) {
		obj.SetObservedGeneration(obj.GetGeneration())
		// Theoretically, the requeue here is not completely accurate either. If we actually update the status, this
		// will trigger another reconciliation rather immediately (or if err somehow is not nil although the condition
		// is ready, we will requeue after exponential backoff). But I guess, we can ignore these edge cases for now!
		if requeue > 0 {
			event.New(recorder, obj, obj.GetVID(), eventv1.EventSeverityInfo, "Reconciliation finished, next run in %s", requeue)
		} else {
			event.New(recorder, obj, obj.GetVID(), eventv1.EventSeverityInfo, "Reconciliation finished, no further runs scheduled until next change")
		}
	}

	// Update the object.
	return patchHelper.Patch(ctx, obj)
}
