package component_version

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	graphPkg "ocm.software/open-component-model/bindings/go/transform/graph"
	graphRuntime "ocm.software/open-component-model/bindings/go/transform/graph/runtime"
	"ocm.software/open-component-model/cli/internal/render/progress"
)

func TestSelectVisualizerUsesSimpleForNonTerminal(t *testing.T) {
	out := &bytes.Buffer{}

	visualiser := selectVisualizer(out)
	require.NotNil(t, visualiser, "visualizer factory should not be nil")

	vis := visualiser(out, 1)
	require.NotNil(t, vis, "visualizer should not be nil")

	actualType := fmt.Sprintf("%T", vis)
	assert.True(t, strings.Contains(actualType, "*simple.simpleVisualizer"),
		"non-terminal output should use simple visualizer, got %q", actualType)
}

func TestVisualizerHandlesEvents(t *testing.T) {
	out := &bytes.Buffer{}

	visualiser := selectVisualizer(out)
	vis := visualiser(out, 1)

	event := progress.Event[*graphPkg.Transformation]{
		ID:    "test-event",
		State: progress.Running,
	}
	vis.HandleEvent(event)

	event.State = progress.Completed
	vis.HandleEvent(event)

	vis.Summary(nil)
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
				Transformation: &graphPkg.Transformation{},
				State:          graphRuntime.Running,
			},
			expectedID:    "",
			expectedState: progress.Running,
		},
		{
			name: "completed state",
			input: graphRuntime.ProgressEvent{
				Transformation: &graphPkg.Transformation{},
				State:          graphRuntime.Completed,
			},
			expectedID:    "",
			expectedState: progress.Completed,
		},
		{
			name: "failed state with error",
			input: graphRuntime.ProgressEvent{
				Transformation: &graphPkg.Transformation{},
				State:          graphRuntime.Failed,
				Err:            testErr,
			},
			expectedID:    "",
			expectedState: progress.Failed,
			expectedErr:   testErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapEvent(tt.input)

			assert.Equal(t, tt.expectedID, result.ID)
			assert.Equal(t, tt.expectedState, result.State)
			assert.Equal(t, tt.input.Transformation, result.Data)
			assert.Equal(t, tt.expectedErr, result.Err)
		})
	}
}
