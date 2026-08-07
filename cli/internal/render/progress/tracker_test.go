package progress

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noopFactory(_ io.Writer, _ int) Visualizer[any] { return &noopVisualizer{} }

func testFactory(vis Visualizer[any]) VisualizerFactory[any] {
	return func(_ io.Writer, _ int) Visualizer[any] { return vis }
}

// --- test helpers ---

type recordingVisualizer struct {
	begins []string
	events []Event[any]
	ends   []error
}

func (v *recordingVisualizer) Begin(name string)            { v.begins = append(v.begins, name) }
func (v *recordingVisualizer) HandleEvent(event Event[any]) { v.events = append(v.events, event) }
func (v *recordingVisualizer) End(err error)                { v.ends = append(v.ends, err) }

type noopVisualizer struct{}

func (n *noopVisualizer) Begin(string)           {}
func (n *noopVisualizer) HandleEvent(Event[any]) {}
func (n *noopVisualizer) End(error)              {}

type logBufferAwareVisualizer struct {
	noopVisualizer
	buf *SyncBuffer
}

func (v *logBufferAwareVisualizer) SetLogBuffer(buf *SyncBuffer) {
	v.buf = buf
}

// --- Tracker tests ---

func TestIsTerminal(t *testing.T) {
	tracker := NewTracker[any](t.Context(), &bytes.Buffer{}, noopFactory)
	assert.False(t, tracker.isTerminal)
	tracker.Stop()

	f, err := os.CreateTemp(t.TempDir(), "test")
	require.NoError(t, err)
	defer f.Close()
	tracker = NewTracker[any](t.Context(), f, noopFactory)
	assert.False(t, tracker.isTerminal)
	tracker.Stop()
}

func TestTracker_NonTerminal_NoSlogRedirect(t *testing.T) {
	originalLogger := slog.Default()
	defer slog.SetDefault(originalLogger)

	tracker := NewTracker[any](t.Context(), &bytes.Buffer{}, noopFactory)
	defer tracker.Stop()

	assert.Equal(t, originalLogger, slog.Default())
}

func TestTracker_StopIsIdempotent(t *testing.T) {
	originalLogger := slog.Default()
	defer slog.SetDefault(originalLogger)

	tracker := NewTracker[any](t.Context(), os.Stdout, noopFactory)
	tracker.Stop()
	tracker.Stop()
}

func TestTracker_LogBufferInjection(t *testing.T) {
	if !NewTracker[any](t.Context(), os.Stdout, noopFactory).isTerminal {
		t.Skip("stdout is not a terminal")
	}
	originalLogger := slog.Default()
	defer slog.SetDefault(originalLogger)

	vis := &logBufferAwareVisualizer{}
	tracker := NewTracker[any](t.Context(), os.Stdout, testFactory(vis))
	defer tracker.Stop()

	_ = tracker.StartOperation("test")

	assert.NotNil(t, vis.buf)
}

// --- Operation tests ---

func TestOperation_Finish_Success(t *testing.T) {
	vis := &recordingVisualizer{}
	tracker := &Tracker[any]{out: &bytes.Buffer{}, isTerminal: true, factory: testFactory(vis)}

	op := tracker.StartOperation("Resolving")
	op.Finish(nil)

	require.Len(t, vis.begins, 1)
	assert.Equal(t, "Resolving", vis.begins[0])
	require.Len(t, vis.ends, 1)
	assert.NoError(t, vis.ends[0])
}

func TestOperation_Finish_Error(t *testing.T) {
	vis := &recordingVisualizer{}
	tracker := &Tracker[any]{out: &bytes.Buffer{}, isTerminal: true, factory: testFactory(vis)}

	testErr := fmt.Errorf("resolve failed")
	op := tracker.StartOperation("Resolving")
	op.Finish(testErr)

	require.Len(t, vis.ends, 1)
	assert.Equal(t, testErr, vis.ends[0])
}

func TestOperation_WithEvents(t *testing.T) {
	vis := &recordingVisualizer{}
	tracker := &Tracker[any]{out: &bytes.Buffer{}, isTerminal: true, factory: testFactory(vis)}

	events := make(chan string, 3)
	mapper := func(s string) Event[any] {
		return Event[any]{ID: s, Name: "display-" + s, State: Running}
	}

	op := tracker.StartOperation("Transferring",
		WithEvents(events, mapper, 3))
	events <- "item1"
	events <- "item2"
	events <- "item3"
	close(events)
	op.Finish(nil)

	require.Len(t, vis.begins, 1)
	assert.Equal(t, "Transferring", vis.begins[0])
	require.Len(t, vis.events, 3)
	assert.Equal(t, "display-item1", vis.events[0].Name)
	require.Len(t, vis.ends, 1)
	assert.NoError(t, vis.ends[0])
}

func TestOperation_WithEvents_CancelledContext(t *testing.T) {
	vis := &recordingVisualizer{}
	tracker := &Tracker[any]{out: &bytes.Buffer{}, isTerminal: true, factory: testFactory(vis)}

	events := make(chan string, 1)
	mapper := func(s string) Event[any] {
		return Event[any]{ID: s, Name: s, State: Failed, Err: context.Canceled}
	}

	op := tracker.StartOperation("Transferring",
		WithEvents(events, mapper, 1))
	events <- "item1"
	close(events)
	op.Finish(nil)

	require.Len(t, vis.events, 1)
	assert.Equal(t, Cancelled, vis.events[0].State)
	assert.Nil(t, vis.events[0].Err)
}

func TestOperation_NonTerminal(t *testing.T) {
	tracker := NewTracker[any](t.Context(), &bytes.Buffer{}, noopFactory)
	defer tracker.Stop()

	op := tracker.StartOperation("Test")
	op.Finish(nil)
}

func TestOperation_NonTerminal_WithEvents(t *testing.T) {
	tracker := NewTracker[any](t.Context(), &bytes.Buffer{}, noopFactory)
	defer tracker.Stop()

	events := make(chan string, 1)
	mapper := func(s string) Event[any] {
		return Event[any]{ID: s, Name: s, State: Running}
	}

	op := tracker.StartOperation("Test",
		WithEvents(events, mapper, 1))
	events <- "item1"
	close(events)
	op.Finish(nil)
}
