package status

import (
	"context"
	"errors"
	"fmt"
	"time"

	kuberecorder "k8s.io/client-go/tools/record"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

// PatchTimeout sets when the defer patch operation should time out.
const PatchTimeout = 30 * time.Second

// PatchContext detaches the status patch from the reconcile context.
// If the reconcile times out, the context for the Patch is already done. We can't use that.
// Which means any Patch operation to update the Status would immediately just fail.
// Therefore, we create an independent context with a PatchTimeout of its own.
func PatchContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), PatchTimeout)
}

// MarkTimedOut marks obj not ready with [v1alpha1.ReconcileTimeoutReason] when
// ctx ran into its deadline and the reconcile failed.
func MarkTimedOut(ctx context.Context, recorder kuberecorder.EventRecorder, obj IdentifiableClientObject, err error) bool {
	if err == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return false
	}
	MarkNotReady(recorder, obj, v1alpha1.ReconcileTimeoutReason,
		fmt.Sprintf("reconciliation aborted by the reconcile timeout: %s", err))
	return true
}
