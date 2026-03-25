package status

import (
	"fmt"

	eventv1 "github.com/fluxcd/pkg/apis/event/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kuberecorder "k8s.io/client-go/tools/record"

	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/event"
)

// MarkNotReady sets the condition status of an Object to `Not Ready`.
func MarkNotReady(recorder kuberecorder.EventRecorder, obj IdentifiableClientObject, reason, msg string) {
	RemoveCondition(obj, v1alpha1.ReconcilingCondition)
	SetCondition(obj, metav1.Condition{
		Type:    v1alpha1.ReadyCondition,
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: msg,
	})
	event.New(recorder, obj, nil, eventv1.EventSeverityError, msg)
}

// MarkAsStalled sets the condition status of an Object to `Stalled`.
func MarkAsStalled(recorder kuberecorder.EventRecorder, obj IdentifiableClientObject, reason, msg string) {
	RemoveCondition(obj, v1alpha1.ReconcilingCondition)
	SetCondition(obj, metav1.Condition{
		Type:    v1alpha1.ReadyCondition,
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: msg,
	})
	SetCondition(obj, metav1.Condition{
		Type:    v1alpha1.StalledCondition,
		Status:  metav1.ConditionTrue,
		Reason:  reason,
		Message: msg,
	})
	event.New(recorder, obj, nil, eventv1.EventSeverityError, msg)
}

// MarkReady sets the condition status of an Object to `Ready`.
func MarkReady(recorder kuberecorder.EventRecorder, obj IdentifiableClientObject, msg string, messageArgs ...any) {
	RemoveCondition(obj, v1alpha1.ReconcilingCondition)
	SetCondition(obj, metav1.Condition{
		Type:    v1alpha1.ReadyCondition,
		Status:  metav1.ConditionTrue,
		Reason:  v1alpha1.SucceededReason,
		Message: fmt.Sprintf(msg, messageArgs...),
	})
	event.New(recorder, obj, nil, eventv1.EventSeverityInfo, msg, messageArgs...)
}
