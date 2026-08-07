package component_version

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	graphPkg "ocm.software/open-component-model/bindings/go/transform/graph"
	graphRuntime "ocm.software/open-component-model/bindings/go/transform/graph/runtime"

	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
	"ocm.software/open-component-model/cli/internal/render/progress"
	"ocm.software/open-component-model/cli/internal/render/progress/bar"
)

func TestNewProgressTracker(t *testing.T) {
	tracker := progress.NewTracker(t.Context(), &bytes.Buffer{}, bar.NewVisualizer[any])
	defer tracker.Stop()
	require.NotNil(t, tracker)
}

func TestSimplePhaseLifecycle(t *testing.T) {
	out := &bytes.Buffer{}
	tracker := progress.NewTracker(t.Context(), out, bar.NewVisualizer[any])
	defer tracker.Stop()

	op := tracker.StartOperation("Test phase")
	op.Finish(nil)
}

func TestMapEvent(t *testing.T) {
	testErr := fmt.Errorf("test error")
	tests := []struct {
		name          string
		input         graphRuntime.ProgressEvent
		expectedID    string
		expectedState progress.State
		expectedErr   error
	}{
		{
			name: "running state",
			input: graphRuntime.ProgressEvent{
				Transformation: &graphPkg.Transformation{
					GenericTransformation: v1alpha1.GenericTransformation{
						TransformationMeta: meta.TransformationMeta{
							Type: runtime.Type{Name: "GetLocalResource"},
							ID:   "transform1",
						},
					},
				},
				State: graphRuntime.Running,
			},
			expectedID:    "transform1",
			expectedState: progress.Running,
		},
		{
			name: "completed state",
			input: graphRuntime.ProgressEvent{
				Transformation: &graphPkg.Transformation{
					GenericTransformation: v1alpha1.GenericTransformation{
						TransformationMeta: meta.TransformationMeta{
							Type: runtime.Type{Name: "AddComponentVersion"},
							ID:   "transform2",
						},
					},
				},
				State: graphRuntime.Completed,
			},
			expectedID:    "transform2",
			expectedState: progress.Completed,
		},
		{
			name: "failed state with error",
			input: graphRuntime.ProgressEvent{
				Transformation: &graphPkg.Transformation{
					GenericTransformation: v1alpha1.GenericTransformation{
						TransformationMeta: meta.TransformationMeta{
							Type: runtime.Type{Name: "AddOCIArtifact"},
							ID:   "transform3",
						},
					},
				},
				State: graphRuntime.Failed,
				Err:   testErr,
			},
			expectedID:    "transform3",
			expectedState: progress.Failed,
			expectedErr:   testErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapEvent(tt.input)

			assert.Equal(t, tt.expectedID, result.ID)
			assert.Equal(t, tt.expectedState, result.State)
			assert.Equal(t, tt.expectedErr, result.Err)
			assert.Contains(t, result.Name, tt.expectedID)
		})
	}
}

func TestMapEvent_NameFormattedAsIDAndType(t *testing.T) {
	input := graphRuntime.ProgressEvent{
		Transformation: &graphPkg.Transformation{
			GenericTransformation: v1alpha1.GenericTransformation{
				TransformationMeta: meta.TransformationMeta{
					Type: runtime.Type{Name: "AddComponentVersion"},
					ID:   "myTransform",
				},
			},
		},
		State: graphRuntime.Running,
	}

	result := mapEvent(input)
	assert.Equal(t, "myTransform [AddComponentVersion]", result.Name)
}
