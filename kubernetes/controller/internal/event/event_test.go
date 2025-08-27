package event

import (
	"fmt"
	"testing"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"
	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/tools/record"

	eventv1 "github.com/fluxcd/pkg/apis/event/v1beta1"

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
			conditions.MarkStalled(obj, v1alpha1.CheckVersionFailedReason, "err")
			conditions.MarkFalse(obj, meta.ReadyCondition, v1alpha1.CheckVersionFailedReason, "err")

			New(recorder, obj, nil, tt.severity, "msg")

			close(recorder.Events)
			for e := range recorder.Events {
				assert.Contains(t, e, "CheckVersionFailed")
				assert.Contains(t, e, tt.expected)
			}
		})
	}
}
