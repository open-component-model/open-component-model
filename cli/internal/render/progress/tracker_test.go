package progress

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// --- Test Helpers ---

// testContent simulates transformation data with an identity.
type testContent struct {
	identity runtime.Identity
}

func (c testContent) GetIdentity() runtime.Identity { return c.identity }

// testRawEvent simulates a raw event from a progress source.
type testRawEvent struct {
	content testContent
	state   State
	err     error
}

// mockVisualizer collects events for verification.
type mockVisualizer struct {
	events []Event[testContent]
}

func (m *mockVisualizer) HandleEvent(e Event[testContent]) {
	m.events = append(m.events, e)
}

func (m *mockVisualizer) Summary(_ error) {}

// mockLogBufferVisualizer implements both Visualizer and LogBufferAware.
type mockLogBufferVisualizer struct {
	mockVisualizer
	logBuffer *bytes.Buffer
}

func (m *mockLogBufferVisualizer) SetLogBuffer(buf *bytes.Buffer) {
	m.logBuffer = buf
}

// newEvent creates a test event with the given state.
func newEvent(name string, state State, err error) testRawEvent {
	return testRawEvent{
		content: testContent{identity: runtime.Identity{"name": name}},
		state:   state,
		err:     err,
	}
}

// mapEvent converts raw events to progress events.
func mapEvent(e testRawEvent) Event[testContent] {
	return Event[testContent]{Data: e.content, State: e.state, Err: e.err}
}

// newTestTracker creates a tracker with a mock visualizer for testing.
func newTestTracker(events chan testRawEvent, vis *mockVisualizer) *Tracker[testContent, testRawEvent] {
	return NewTracker(
		WithEvents(events, mapEvent),
		WithVisualizer[testContent, testRawEvent](func(_ io.Writer, _ int) Visualizer[testContent] { return vis }),
	)
}

// --- Tests ---

func TestTracker_PreservesErrorInFailedEvents(t *testing.T) {
	// Failed events should preserve the error for display.
	events := make(chan testRawEvent, 10)
	vis := &mockVisualizer{}
	tracker := newTestTracker(events, vis)

	go tracker.Start(t.Context())

	testErr := errors.New("connection timeout")
	events <- newEvent("failing-task", Failed, testErr)
	close(events)
	tracker.Summary(nil)

	require.Len(t, vis.events, 1, "should receive 1 event")
	assert.Equal(t, Failed, vis.events[0].State, "event should be Failed")
	assert.Equal(t, testErr, vis.events[0].Err, "error should be preserved")
}

func TestTracker_ContextCancelledChangesStateToCancelled(t *testing.T) {
	// Events with context.Canceled error should have state changed to Cancelled.
	events := make(chan testRawEvent, 10)
	vis := &mockVisualizer{}
	tracker := newTestTracker(events, vis)

	go tracker.Start(t.Context())

	events <- newEvent("cancelled-task", Running, context.Canceled)
	close(events)
	tracker.Summary(nil)

	require.Len(t, vis.events, 1, "should receive 1 event")
	assert.Equal(t, Cancelled, vis.events[0].State, "state should be changed to Cancelled")
	assert.Nil(t, vis.events[0].Err, "error should be cleared")
}

func TestTracker_SlogInterception(t *testing.T) {
	t.Run("redirects slog to buffer", func(t *testing.T) {
		events := make(chan testRawEvent, 10)
		vis := &mockLogBufferVisualizer{}
		tracker := NewTracker(
			WithEvents(events, mapEvent),
			WithVisualizer[testContent, testRawEvent](func(_ io.Writer, _ int) Visualizer[testContent] { return vis }),
		)

		go tracker.Start(t.Context())

		events <- newEvent("task", Running, nil)
		slog.Info("intercepted message")

		close(events)
		tracker.Summary(nil)

		require.NotNil(t, vis.logBuffer, "log buffer should be set on LogBufferAware visualizer")
	})

	t.Run("restores previous logger after summary", func(t *testing.T) {
		originalLogger := slog.Default()
		defer slog.SetDefault(originalLogger)

		events := make(chan testRawEvent, 10)
		vis := &mockLogBufferVisualizer{}
		tracker := NewTracker(
			WithEvents(events, mapEvent),
			WithVisualizer[testContent, testRawEvent](func(_ io.Writer, _ int) Visualizer[testContent] { return vis }),
		)

		go tracker.Start(t.Context())
		close(events)
		tracker.Summary(nil)

		assert.Equal(t, originalLogger, slog.Default(), "Summary should restore the original logger")
	})
}

func TestIsTerminal(t *testing.T) {
	t.Run("returns false for buffer", func(t *testing.T) {
		buf := &bytes.Buffer{}
		assert.False(t, IsTerminal(buf), "bytes.Buffer is not a terminal")
	})

	t.Run("returns false for regular file", func(t *testing.T) {
		f, err := os.CreateTemp(t.TempDir(), "test")
		require.NoError(t, err)
		defer f.Close()
		assert.False(t, IsTerminal(f), "regular file is not a terminal")
	})

	t.Run("returns true for real terminal", func(t *testing.T) {
		if !IsTerminal(os.Stdout) {
			t.Skip("stdout is not a terminal (e.g., CI environment)")
		}
		assert.True(t, IsTerminal(os.Stdout))
	})
}
