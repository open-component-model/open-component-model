package bar

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/cli/internal/render/progress"
)

// newTestVisualizer creates a barVisualizer for testing without reserveSpace side effects.
func newTestVisualizer(total int) (*barVisualizer[string], *bytes.Buffer) {
	buf := &bytes.Buffer{}
	v := &barVisualizer[string]{
		out:            buf,
		total:          total,
		events:         make([]progress.Event[string], 0, total),
		done:           make(chan struct{}),
		maxLogs:        4,
		errorFormatter: func(_ string, err error) string { return err.Error() },
	}
	return v, buf
}

func TestHandleEvent_OrderTracking(t *testing.T) {
	t.Run("first event adds to events", func(t *testing.T) {
		v, _ := newTestVisualizer(3)

		v.HandleEvent(progress.Event[string]{ID: "item1", State: progress.Running})

		assert.Len(t, v.events, 1, "events should have 1 entry")
		assert.Equal(t, "item1", v.events[0].ID)
	})

	t.Run("same ID does not duplicate", func(t *testing.T) {
		v, _ := newTestVisualizer(3)

		// First event - Running
		v.HandleEvent(progress.Event[string]{ID: "item1", State: progress.Running})
		// Second event - Completed (same ID)
		v.HandleEvent(progress.Event[string]{ID: "item1", State: progress.Completed})

		assert.Len(t, v.events, 1, "events should still have 1 entry")
		assert.Equal(t, progress.Completed, v.events[0].State, "item1 should be Completed")
	})

	t.Run("different IDs added in order", func(t *testing.T) {
		v, _ := newTestVisualizer(3)

		v.HandleEvent(progress.Event[string]{ID: "item1", State: progress.Running})
		v.HandleEvent(progress.Event[string]{ID: "item2", State: progress.Running})
		v.HandleEvent(progress.Event[string]{ID: "item3", State: progress.Running})

		assert.Len(t, v.events, 3)
		assert.Equal(t, "item1", v.events[0].ID)
		assert.Equal(t, "item2", v.events[1].ID)
		assert.Equal(t, "item3", v.events[2].ID)
	})

	t.Run("completed item not updated again", func(t *testing.T) {
		v, _ := newTestVisualizer(3)

		v.HandleEvent(progress.Event[string]{ID: "item1", State: progress.Running})
		v.HandleEvent(progress.Event[string]{ID: "item1", State: progress.Completed})
		// Try to update again - should be ignored
		v.HandleEvent(progress.Event[string]{ID: "item1", State: progress.Failed})

		assert.Equal(t, progress.Completed, v.events[0].State, "item1 should remain Completed")
	})
}

func TestRenderLogs_OutputFormat(t *testing.T) {
	t.Run("renders single item", func(t *testing.T) {
		v, buf := newTestVisualizer(3)
		v.events = []progress.Event[string]{{ID: "item1", State: progress.Completed}}

		v.renderLogs()

		output := buf.String()
		newlineCount := strings.Count(output, "\n")
		assert.Equal(t, 4, newlineCount, "should have maxLogs newlines")
		assert.Contains(t, output, "✓", "output should have checkmark")
		assert.Contains(t, output, "item1", "output should contain item1")
	})

	t.Run("renders multiple items in order", func(t *testing.T) {
		v, buf := newTestVisualizer(3)
		v.events = []progress.Event[string]{
			{ID: "item1", State: progress.Completed},
			{ID: "item2", State: progress.Running},
			{ID: "item3", State: progress.Failed},
		}

		v.renderLogs()

		output := buf.String()
		newlineCount := strings.Count(output, "\n")
		assert.Equal(t, 4, newlineCount, "should have maxLogs newlines")

		// Verify order by checking positions
		pos1 := strings.Index(output, "item1")
		pos2 := strings.Index(output, "item2")
		pos3 := strings.Index(output, "item3")
		assert.True(t, pos1 < pos2, "item1 should appear before item2")
		assert.True(t, pos2 < pos3, "item2 should appear before item3")
	})

	t.Run("only shows last maxLogs items when exceeding limit", func(t *testing.T) {
		v, buf := newTestVisualizer(6)
		v.events = []progress.Event[string]{
			{ID: "item1", State: progress.Completed},
			{ID: "item2", State: progress.Completed},
			{ID: "item3", State: progress.Completed},
			{ID: "item4", State: progress.Completed},
			{ID: "item5", State: progress.Completed},
			{ID: "item6", State: progress.Completed},
		}

		v.renderLogs()

		output := buf.String()
		newlineCount := strings.Count(output, "\n")
		assert.Equal(t, 4, newlineCount, "should have maxLogs newlines")

		// Should NOT show first 2 items
		assert.NotContains(t, output, "item1")
		assert.NotContains(t, output, "item2")
		// Should show last 4 items
		assert.Contains(t, output, "item3")
		assert.Contains(t, output, "item4")
		assert.Contains(t, output, "item5")
		assert.Contains(t, output, "item6")
	})
}

func TestRenderLogs_StateTransitions(t *testing.T) {
	t.Run("item updates from running to completed", func(t *testing.T) {
		v, buf := newTestVisualizer(1)

		// Initial state - Running
		v.HandleEvent(progress.Event[string]{ID: "item1", State: progress.Running})
		firstOutput := buf.String()
		buf.Reset()

		// Update to Completed
		v.HandleEvent(progress.Event[string]{ID: "item1", State: progress.Completed})
		secondOutput := buf.String()

		// Events should have exactly 1 entry
		assert.Len(t, v.events, 1, "events should have exactly 1 entry")

		// First output should show running icon
		assert.Contains(t, firstOutput, "⏳", "first render should show running icon")

		// Second output should show completed icon
		assert.Contains(t, secondOutput, "✓", "second render should show completed icon")
	})
}

func TestFormatItem(t *testing.T) {
	tests := []struct {
		name  string
		state progress.State
		icon  string
	}{
		{"running shows hourglass", progress.Running, "⏳"},
		{"completed shows checkmark", progress.Completed, "✓"},
		{"failed shows X", progress.Failed, "✗"},
		{"cancelled shows circle", progress.Cancelled, "⊘"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, _ := newTestVisualizer(1)
			event := progress.Event[string]{ID: "test", State: tt.state}

			result := v.formatItem(event)

			assert.Contains(t, result, tt.icon)
			assert.Contains(t, result, "test")
		})
	}
}

func TestFormatItem_WithFormatter(t *testing.T) {
	v, _ := newTestVisualizer(1)
	v.formatter = func(data string) string {
		return "formatted-" + data
	}

	event := progress.Event[string]{ID: "original", Data: "mydata", State: progress.Completed}
	result := v.formatItem(event)

	assert.Contains(t, result, "formatted-mydata")
	assert.NotContains(t, result, "original")
}

func TestLogBuffer(t *testing.T) {
	t.Run("drain prints and clears buffer", func(t *testing.T) {
		v, out := newTestVisualizer(1)
		logBuf := &bytes.Buffer{}
		v.SetLogBuffer(logBuf)

		logBuf.WriteString("line one\nline two\n")
		v.drainLogBuffer()

		output := out.String()
		assert.Contains(t, output, "line one", "should print first log line")
		assert.Contains(t, output, "line two", "should print second log line")
		assert.Equal(t, 0, logBuf.Len(), "log buffer should be drained")
	})

	t.Run("drain is noop when buffer is nil", func(t *testing.T) {
		v, out := newTestVisualizer(1)

		v.drainLogBuffer()
		assert.Empty(t, out.String(), "should not print anything with nil buffer")
	})

	t.Run("drain is noop when buffer is empty", func(t *testing.T) {
		v, out := newTestVisualizer(1)
		v.SetLogBuffer(&bytes.Buffer{})

		v.drainLogBuffer()
		assert.Empty(t, out.String(), "should not print anything with empty buffer")
	})
}
