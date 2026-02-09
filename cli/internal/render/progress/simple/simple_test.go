package simple_test

import (
	"bytes"
	"log/slog"
	"testing"

	"ocm.software/open-component-model/cli/internal/render/progress"
	"ocm.software/open-component-model/cli/internal/render/progress/simple"
)

func TestSimpleVisualizer(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	factory := simple.NewSimpleVisualizer[string](logger)
	vis := factory(nil, 0)

	// Test running event
	vis.HandleEvent(progress.Event[string]{
		ID:    "test-1",
		State: progress.Running,
		Data:  "test data",
	})

	// Test completed event
	vis.HandleEvent(progress.Event[string]{
		ID:    "test-2",
		State: progress.Completed,
		Data:  "test data",
	})

	// Test failed event
	vis.HandleEvent(progress.Event[string]{
		ID:    "test-3",
		State: progress.Failed,
		Data:  "test data",
		Err:   nil,
	})

	vis.Summary(nil)

	output := buf.String()
	if output == "" {
		t.Error("Expected log output, got empty string")
	}
}
