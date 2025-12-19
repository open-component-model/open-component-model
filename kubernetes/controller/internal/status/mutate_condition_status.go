package status

import (
	eventv1 "github.com/fluxcd/pkg/apis/event/v1beta1"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"
	kuberecorder "k8s.io/client-go/tools/record"

	"ocm.software/open-component-model/kubernetes/controller/internal/event"
)

// MarkNotReady sets the condition status of an Object to `Not Ready`.
func MarkNotReady(recorder kuberecorder.EventRecorder, obj conditions.Setter, reason, msg string) {
	conditions.Delete(obj, meta.ReconcilingCondition)
	conditions.MarkFalse(obj, meta.ReadyCondition, reason, "%s", msg)
	event.New(recorder, obj, nil, eventv1.EventSeverityError, msg)
}

// MarkAsStalled sets the condition status of an Object to `Stalled`.
func MarkAsStalled(recorder kuberecorder.EventRecorder, obj conditions.Setter, reason, msg string) {
	conditions.Delete(obj, meta.ReconcilingCondition)
	conditions.MarkFalse(obj, meta.ReadyCondition, reason, "%s", msg)
	conditions.MarkStalled(obj, reason, "%s", msg)
	event.New(recorder, obj, nil, eventv1.EventSeverityError, msg)
}

// MarkReady sets the condition status of an Object to `Ready`.
func MarkReady(recorder kuberecorder.EventRecorder, obj conditions.Setter, msg string, messageArgs ...any) {
	conditions.Delete(obj, meta.ReconcilingCondition)
	conditions.MarkTrue(obj, meta.ReadyCondition, meta.SucceededReason, msg, messageArgs...)
	event.New(recorder, obj, nil, eventv1.EventSeverityInfo, msg, messageArgs...)
}
