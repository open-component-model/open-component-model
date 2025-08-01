package event

import (
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"

	eventv1 "github.com/fluxcd/pkg/apis/event/v1beta1"
	corev1 "k8s.io/api/core/v1"
	kuberecorder "k8s.io/client-go/tools/record"
)

func New(recorder kuberecorder.EventRecorder, obj conditions.Getter, metadata map[string]string, severity, msg string, args ...any) {
	if metadata == nil {
		metadata = map[string]string{}
	}

	reason := severity
	if r := conditions.GetReason(obj, meta.ReadyCondition); r != "" {
		reason = r
	}

	eventType := corev1.EventTypeNormal
	if severity == eventv1.EventSeverityError {
		eventType = corev1.EventTypeWarning
	}

	recorder.AnnotatedEventf(obj, metadata, eventType, reason, msg, args...)
}
