package status_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/status"
)

// stalled builds an object already in a terminal Stalled state.
func stalled() *v1alpha1.Discovery {
	obj := &v1alpha1.Discovery{}
	status.SetCondition(obj, metav1.Condition{
		Type:   v1alpha1.ReadyCondition,
		Status: metav1.ConditionFalse,
		Reason: v1alpha1.SelectorFailedReason,
	})
	status.SetCondition(obj, metav1.Condition{
		Type:   v1alpha1.StalledCondition,
		Status: metav1.ConditionTrue,
		Reason: v1alpha1.SelectorFailedReason,
	})
	return obj
}

func TestMarkReadyClearsStalled(t *testing.T) {
	r := require.New(t)
	obj := stalled()

	status.MarkReady(record.NewFakeRecorder(4), obj, "recovered")

	r.False(status.IsStalled(obj), "recovery must clear the terminal Stalled condition")
	ready := status.FindCondition(obj, v1alpha1.ReadyCondition)
	r.NotNil(ready)
	r.Equal(metav1.ConditionTrue, ready.Status)
}

func TestMarkNotReadyClearsStalled(t *testing.T) {
	r := require.New(t)
	obj := stalled()

	status.MarkNotReady(record.NewFakeRecorder(4), obj, v1alpha1.ResourceIsNotAvailable, "retrying")

	r.False(status.IsStalled(obj), "a retryable failure is no longer terminal")
	ready := status.FindCondition(obj, v1alpha1.ReadyCondition)
	r.NotNil(ready)
	r.Equal(metav1.ConditionFalse, ready.Status)
	r.Equal(v1alpha1.ResourceIsNotAvailable, ready.Reason)
}
