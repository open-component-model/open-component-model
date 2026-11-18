package status

import (
	"context"
	"errors"
	"fmt"
	"time"

	kuberecorder "k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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
// ctx ran into its deadline and the reconcile failed, and returns the error the
// reconcile should report. If a step returned with a TerminalError that wraps the deadline, such as, a CEL stage
// we need to strip that part from the error because otherwise, a timeout will not be retried
// as intended. If the attached context did not time out, we leave the error intact.
func MarkTimedOut(ctx context.Context, recorder kuberecorder.EventRecorder, obj IdentifiableClientObject, err error) error {
	if err == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return err
	}
	// A terminal error the deadline did not cause fails the same way on a retry, so it stays terminal.
	if errors.Is(err, reconcile.TerminalError(nil)) && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	MarkNotReady(recorder, obj, v1alpha1.ReconcileTimeoutReason,
		fmt.Sprintf("reconciliation aborted by the reconcile timeout: %s", err))
	// Flattening err to its text drops any reconcile.TerminalError marker; the deadline stays matchable.
	return fmt.Errorf("reconciliation aborted by the reconcile timeout: %s: %w", err.Error(), context.DeadlineExceeded)
}
