package event

import (
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kuberecorder "k8s.io/client-go/tools/record"

	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

// EventObject is an object that can be used with the event recorder and
// carries conditions for deriving the event reason.
type EventObject interface {
	runtime.Object
	GetConditions() []metav1.Condition
	GetNamespace() string
	GetName() string
}

func New(recorder kuberecorder.EventRecorder, obj EventObject, metadata map[string]string, severity, msg string, args ...any) {
	if metadata == nil {
		metadata = map[string]string{}
	}

	reason := severity
	if cond := apimeta.FindStatusCondition(obj.GetConditions(), v1alpha1.ReadyCondition); cond != nil && cond.Reason != "" {
		reason = cond.Reason
	}

	eventType := corev1.EventTypeNormal
	if severity == v1alpha1.EventSeverityError {
		eventType = corev1.EventTypeWarning
	}

	recorder.AnnotatedEventf(obj, metadata, eventType, reason, msg, args...)
}
