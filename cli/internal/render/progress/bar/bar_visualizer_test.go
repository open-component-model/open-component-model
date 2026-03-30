package bar

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/cli/internal/render/progress"
)

func newTestVisualizer(total int) (*barVisualizer[string], *bytes.Buffer) {
	buf := &bytes.Buffer{}
	v := &barVisualizer[string]{
		out:            buf,
		total:          total,
		events:         make([]progress.Event[string], 0, total),
		done:           make(chan struct{}),
		maxLogs:        min(4, total),
		errorFormatter: func(_ string, err error) string { return err.Error() },
	}
	return v, buf
}

func TestHandleEvent_OrderTracking(t *testing.T) {
	t.Run("first event adds to events", func(t *testing.T) {
		v, _ := newTestVisualizer(3)

		v.HandleEvent(progress.Event[string]{ID: "item1", State: progress.Running})

		assert.Len(t, v.events, 1)
		assert.Equal(t, "item1", v.events[0].ID)
	})

	t.Run("same ID does not duplicate", func(t *testing.T) {
		v, _ := newTestVisualizer(3)

		v.HandleEvent(progress.Event[string]{ID: "item1", State: progress.Running})
		v.HandleEvent(progress.Event[string]{ID: "item1", State: progress.Completed})

		assert.Len(t, v.events, 1)
		assert.Equal(t, progress.Completed, v.events[0].State)
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
		v.HandleEvent(progress.Event[string]{ID: "item1", State: progress.Failed})

		assert.Equal(t, progress.Completed, v.events[0].State)
	})
}

func TestRenderLogs_OutputFormat(t *testing.T) {
	t.Run("renders single item", func(t *testing.T) {
		v, buf := newTestVisualizer(3)
		v.events = []progress.Event[string]{{ID: "item1", Name: "item1", State: progress.Completed}}

		v.renderLogs()

		output := buf.String()
		assert.Equal(t, 3, strings.Count(output, "\n"))
		assert.Contains(t, output, "✓")
		assert.Contains(t, output, "item1")
	})

	t.Run("only shows last maxLogs items when exceeding limit", func(t *testing.T) {
		v, buf := newTestVisualizer(6)
		v.events = []progress.Event[string]{
			{ID: "item1", Name: "item1", State: progress.Completed},
			{ID: "item2", Name: "item2", State: progress.Completed},
			{ID: "item3", Name: "item3", State: progress.Completed},
			{ID: "item4", Name: "item4", State: progress.Completed},
			{ID: "item5", Name: "item5", State: progress.Completed},
			{ID: "item6", Name: "item6", State: progress.Completed},
		}

		v.renderLogs()

		output := buf.String()
		assert.NotContains(t, output, "item1")
		assert.NotContains(t, output, "item2")
		assert.Contains(t, output, "item3")
		assert.Contains(t, output, "item6")
	})
}

func TestFormatItem(t *testing.T) {
	tests := []struct {
		name  string
		state progress.State
		icon  string
	}{
		{"running shows spinner", progress.Running, SpinnerFrames[0]},
		{"completed shows checkmark", progress.Completed, "✓"},
		{"failed shows X", progress.Failed, "✗"},
		{"cancelled shows circle", progress.Cancelled, "⊘"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, _ := newTestVisualizer(1)
			result := v.formatItem(progress.Event[string]{ID: "test", Name: "test", State: tt.state})
			assert.Contains(t, result, tt.icon)
			assert.Contains(t, result, "test")
		})
	}
}

func TestFormatItem_FallsBackToID(t *testing.T) {
	v, _ := newTestVisualizer(1)
	result := v.formatItem(progress.Event[string]{ID: "my-id", State: progress.Completed})
	assert.Contains(t, result, "my-id")
}

func TestLogBuffer(t *testing.T) {
	t.Run("drain prints and clears buffer", func(t *testing.T) {
		v, out := newTestVisualizer(1)
		logBuf := &progress.SyncBuffer{}
		v.SetLogBuffer(logBuf)

		logBuf.Write([]byte("line one\nline two\n"))
		v.drainLogBuffer()

		assert.Contains(t, out.String(), "line one")
		assert.Contains(t, out.String(), "line two")
		assert.Equal(t, 0, logBuf.Len())
	})

	t.Run("drain is noop when buffer is nil", func(t *testing.T) {
		v, out := newTestVisualizer(1)
		v.drainLogBuffer()
		assert.Empty(t, out.String())
	})
}

func TestRenderFinalHeader(t *testing.T) {
	t.Run("success shows checkmark", func(t *testing.T) {
		v, buf := newTestVisualizer(1)
		v.header = "Transferring"
		v.events = []progress.Event[string]{{ID: "item1", State: progress.Completed}}

		v.End(nil)

		assert.Contains(t, buf.String(), "✓")
		assert.Contains(t, buf.String(), "Transferring...")
	})

	t.Run("error shows X", func(t *testing.T) {
		v, buf := newTestVisualizer(1)
		v.header = "Transferring"

		v.End(assert.AnError)

		assert.Contains(t, buf.String(), "✗")
	})

	t.Run("failed event shows X even without error", func(t *testing.T) {
		v, buf := newTestVisualizer(1)
		v.header = "Transferring"
		v.events = []progress.Event[string]{{ID: "item1", State: progress.Failed}}

		v.End(nil)

		assert.Contains(t, buf.String(), "✗")
	})
}

// --- NewVisualizer factory tests ---

func TestNewVisualizer(t *testing.T) {
	buf := &bytes.Buffer{}
	vis := NewVisualizer[any](buf, 3)

	vis.Begin("Transfer")
	bv := vis.(*barVisualizer[any])
	close(bv.done)
	bv.done = make(chan struct{})
	bv.HandleEvent(progress.Event[any]{ID: "a", Name: "item-a", State: progress.Completed})
	buf.Reset()
	vis.End(nil)

	output := buf.String()
	assert.Contains(t, output, "✓")
	assert.Contains(t, output, "Transfer")
}

func TestNewVisualizer_Simple(t *testing.T) {
	buf := &bytes.Buffer{}
	vis := NewVisualizer[any](buf, 0)

	vis.Begin("Loading")
	bv := vis.(*barVisualizer[any])
	close(bv.done)
	bv.done = make(chan struct{})

	buf.Reset()
	vis.End(nil)

	output := buf.String()
	assert.Contains(t, output, "✓")
	assert.Contains(t, output, "Loading")
	// No progress bar for total=0
	assert.NotContains(t, stripANSI(output), "[")
}

func TestNewVisualizer_SetErrorFormatter(t *testing.T) {
	buf := &bytes.Buffer{}
	vis := NewVisualizer[any](buf, 1)

	formatter := func(data any, err error) string { return "CUSTOM: " + err.Error() }
	if setter, ok := vis.(progress.ErrorFormatterSetter[any]); ok {
		setter.SetErrorFormatter(formatter)
	}

	vis.Begin("Transfer")
	bv := vis.(*barVisualizer[any])
	close(bv.done)
	bv.done = make(chan struct{})
	bv.HandleEvent(progress.Event[any]{ID: "x", Name: "item-x", State: progress.Failed, Err: assert.AnError})
	buf.Reset()
	vis.End(nil)

	assert.Contains(t, buf.String(), "CUSTOM:")
}

// --- renderBar tests ---

func TestRenderBar_ProgressPercentage(t *testing.T) {
	v, buf := newTestVisualizer(4)
	v.events = []progress.Event[string]{
		{ID: "a", State: progress.Completed},
		{ID: "b", State: progress.Completed},
	}

	v.renderBar()
	output := stripANSI(buf.String())

	assert.Contains(t, output, "50%")
	assert.Contains(t, output, "2/4")
}

func TestRenderBar_ShowsFailedCount(t *testing.T) {
	v, buf := newTestVisualizer(3)
	v.events = []progress.Event[string]{
		{ID: "a", State: progress.Completed},
		{ID: "b", State: progress.Failed},
	}

	v.renderBar()
	output := stripANSI(buf.String())

	assert.Contains(t, output, "1 failed")
}

func TestRenderBar_ShowsCancelledCount(t *testing.T) {
	v, buf := newTestVisualizer(3)
	v.events = []progress.Event[string]{
		{ID: "a", State: progress.Completed},
		{ID: "b", State: progress.Cancelled},
	}

	v.renderBar()
	output := stripANSI(buf.String())

	assert.Contains(t, output, "1 cancelled")
}

// --- renderFailureSummary tests ---

func TestRenderFailureSummary_ShowsErrorIDs(t *testing.T) {
	v, buf := newTestVisualizer(2)
	v.events = []progress.Event[string]{
		{ID: "task-1", State: progress.Completed},
		{ID: "task-2", State: progress.Failed, Err: assert.AnError},
	}

	v.renderFailureSummary()
	output := stripANSI(buf.String())

	assert.Contains(t, output, "Errors:")
	assert.Contains(t, output, "task-2")
	assert.NotContains(t, output, "task-1")
}
