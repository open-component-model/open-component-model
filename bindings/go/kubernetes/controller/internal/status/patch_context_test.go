package status_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/status"
)

// expired returns a reconcile context whose deadline has already passed.
func expired(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Nanosecond)
	t.Cleanup(cancel)
	<-ctx.Done()
	return ctx
}

func TestPatchContextSurvivesExpiredReconcile(t *testing.T) {
	r := require.New(t)
	ctx, cancel := status.PatchContext(expired(t))
	defer cancel()

	r.NoError(ctx.Err(), "the patch must still run once the reconcile timeout fired")
	deadline, ok := ctx.Deadline()
	r.True(ok, "the patch itself must stay bounded")
	r.WithinDuration(time.Now().Add(status.PatchTimeout), deadline, time.Second)
}

func TestMarkTimedOut(t *testing.T) {
	failure := errors.New("fetching descriptor: context deadline exceeded")

	t.Run("expired reconcile with error marks the object", func(t *testing.T) {
		r := require.New(t)
		obj := stalled()

		got := status.MarkTimedOut(expired(t), record.NewFakeRecorder(4), obj, failure)

		r.ErrorIs(got, context.DeadlineExceeded)
		r.Contains(got.Error(), failure.Error())
		ready := status.FindCondition(obj, v1alpha1.ReadyCondition)
		r.NotNil(ready)
		r.Equal(metav1.ConditionFalse, ready.Status)
		r.Equal(v1alpha1.ReconcileTimeoutReason, ready.Reason)
		r.Contains(ready.Message, failure.Error())
		r.False(status.IsStalled(obj), "a timeout is retried, so it is not terminal")
	})

	t.Run("terminal error aborted by the timeout becomes retryable", func(t *testing.T) {
		// A CEL stage interrupted by the deadline reports a TerminalError; left alone,
		// controller-runtime would never requeue the object.
		r := require.New(t)
		terminal := reconcile.TerminalError(failure)
		r.ErrorIs(terminal, reconcile.TerminalError(nil))

		got := status.MarkTimedOut(expired(t), record.NewFakeRecorder(4), stalled(), terminal)

		r.NotErrorIs(got, reconcile.TerminalError(nil))
		r.ErrorIs(got, context.DeadlineExceeded)
	})

	t.Run("expired reconcile without error is left alone", func(t *testing.T) {
		r := require.New(t)
		obj := stalled()
		r.NoError(status.MarkTimedOut(expired(t), record.NewFakeRecorder(4), obj, nil))
		r.True(status.IsStalled(obj))
	})

	t.Run("canceled reconcile is not a timeout", func(t *testing.T) {
		r := require.New(t)
		obj := stalled()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		r.Same(failure, status.MarkTimedOut(ctx, record.NewFakeRecorder(4), obj, failure))
		r.True(status.IsStalled(obj))
	})

	t.Run("live reconcile is not a timeout", func(t *testing.T) {
		r := require.New(t)
		obj := stalled()
		terminal := reconcile.TerminalError(failure)
		r.Same(terminal, status.MarkTimedOut(t.Context(), record.NewFakeRecorder(4), obj, terminal))
		r.True(status.IsStalled(obj))
	})
}
