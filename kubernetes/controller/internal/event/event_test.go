package event

import (
	"fmt"
	"testing"

	eventv1 "github.com/fluxcd/pkg/apis/event/v1beta1"
	"github.com/stretchr/testify/assert"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

func TestNewEvent(t *testing.T) {
	eventTests := []struct {
		description string
		severity    string
		expected    string
	}{
		{
			description: "event is of type info",
			severity:    eventv1.EventSeverityInfo,
			expected:    "Normal",
		},
		{
			description: "event is of type error",
			severity:    eventv1.EventSeverityError,
			expected:    "Warning",
		},
	}
	for i, tt := range eventTests {
		t.Run(fmt.Sprintf("%d: %s", i, tt.description), func(t *testing.T) {
			recorder := record.NewFakeRecorder(32)
			obj := &v1alpha1.Component{}
			apimeta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
				Type:    v1alpha1.StalledCondition,
				Status:  metav1.ConditionTrue,
				Reason:  v1alpha1.CheckVersionFailedReason,
				Message: "err",
			})
			apimeta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
				Type:    v1alpha1.ReadyCondition,
				Status:  metav1.ConditionFalse,
				Reason:  v1alpha1.CheckVersionFailedReason,
				Message: "err",
			})

			New(recorder, obj, nil, tt.severity, "msg")

			close(recorder.Events)
			for e := range recorder.Events {
				assert.Contains(t, e, "CheckVersionFailed")
				assert.Contains(t, e, tt.expected)
			}
		})
	}
}
