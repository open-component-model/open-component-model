// Package progress provides a generic progress tracking system for long-running operations.
// It receives events from a source, maps them to a common format, and displays them via a Visualizer.
//
// Usage:
//
//	tracker := progress.NewTracker(
//	    progress.WithEvents(eventsChan, mapFunc),
//	    progress.WithVisualizer(bar.NewBarVisualizer()),
//	    progress.WithTotal(10),
//	)
//	go tracker.Start(ctx)
//	// ... send events to eventsChan ...
//	tracker.Summary(nil)
package progress

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"

	"golang.org/x/term"
)

// compile-time check: ensure bufferedHandler implements slog.Handler.
var _ slog.Handler = (*bufferedHandler)(nil)

// State represents the lifecycle state of a tracked item.
type State string

const (
	Running   State = "running"   // Currently processing
	Completed State = "completed" // Finished successfully
	Failed    State = "failed"    // Finished with error
	Cancelled State = "cancelled" // Stopped due to context cancellation
)

// Event holds progress data for a single tracked item.
//   - ID: unique identifier for the item
//   - Data: custom payload (e.g., transformation details)
//   - State: current lifecycle state
//   - Err: error if the item failed
type Event[T any] struct {
	ID    string
	Data  T
	State State
	Err   error
}

// Visualizer displays progress events to the user.
// Implementations handle rendering (e.g., progress bar, simple logs).
type Visualizer[T any] interface {
	// HandleEvent is called for each progress update.
	HandleEvent(event Event[T])
	// Summary is called once after all events are processed.
	Summary(err error)
}

// VisualizerFactory creates a Visualizer with the output writer and total item count.
type VisualizerFactory[T any] func(out io.Writer, total int) Visualizer[T]

// IsTerminal reports whether the writer is connected to a terminal.
func IsTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}

// Tracker connects an event source to a visualizer.
// It reads events from a channel, maps them to Events, and forwards to the visualizer.
//
// Type parameters:
//   - T: the data type stored in Event.Data
//   - E: the raw event type from the source channel
type Tracker[T, E any] struct {
	events            <-chan E             // source of raw events
	finished          chan int             // closed when Start() completes
	out               io.Writer            // output destination
	visualizerFactory VisualizerFactory[T] // creates the visualizer
	visualizer        Visualizer[T]        // active visualizer instance
	mapper            func(E) Event[T]     // converts raw events to Events
	total             int                  // total expected items (for progress %)
	previousLogger    *slog.Logger         // saved logger restored after tracking
	logBuffer         *bytes.Buffer        // buffered slog output during tracking
}

// TrackerOption configures a Tracker using the functional options pattern.
type TrackerOption[T, E any] func(*Tracker[T, E])

// WithVisualizer sets the factory that creates the visualizer.
// The visualizer handles all rendering of progress updates.
func WithVisualizer[T, E any](factory VisualizerFactory[T]) TrackerOption[T, E] {
	return func(t *Tracker[T, E]) {
		t.visualizerFactory = factory
	}
}

// WithEvents sets the event source channel and mapper function.
// The mapper converts raw events (E) to progress Events (T).
func WithEvents[T, E any](events <-chan E, f func(E) Event[T]) TrackerOption[T, E] {
	return func(t *Tracker[T, E]) {
		t.events = events
		t.mapper = f
	}
}

// WithTotal sets the expected number of items for progress percentage calculation.
func WithTotal[T, E any](total int) TrackerOption[T, E] {
	return func(t *Tracker[T, E]) {
		t.total = total
	}
}

// WithOutput sets where the visualizer writes output (e.g., os.Stdout).
func WithOutput[T, E any](w io.Writer) TrackerOption[T, E] {
	return func(t *Tracker[T, E]) {
		t.out = w
	}
}

// NewTracker creates a Tracker configured with the given options.
func NewTracker[T, E any](opts ...TrackerOption[T, E]) *Tracker[T, E] {
	t := &Tracker[T, E]{
		finished: make(chan int),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Start reads events from the channel and forwards them to the visualizer.
// Run this in a goroutine. It returns when the event channel is closed.
// Context cancellation errors are automatically converted to Cancelled state.
func (t *Tracker[T, E]) Start(ctx context.Context) {
	defer close(t.finished)
	if t.visualizerFactory != nil {
		t.visualizer = t.visualizerFactory(t.out, t.total)
	}
	t.interceptSlog()
	for evt := range t.events {
		mappedEvt := t.mapper(evt)
		if mappedEvt.Err != nil && (errors.Is(mappedEvt.Err, context.Canceled) || errors.Is(mappedEvt.Err, context.DeadlineExceeded)) {
			mappedEvt.State = Cancelled
			mappedEvt.Err = nil
		}
		if t.visualizer != nil {
			t.visualizer.HandleEvent(mappedEvt)
		}
	}
}

// interceptSlog redirects the default slog logger to a buffer if the visualizer supports it.
// Not thread-safe: uses slog.SetDefault, so only one Tracker should be active at a time.
func (t *Tracker[T, E]) interceptSlog() {
	if lba, ok := t.visualizer.(LogBufferAware); ok {
		t.previousLogger = slog.Default()
		t.logBuffer = &bytes.Buffer{}
		slog.SetDefault(slog.New(newBufferedHandler(t.logBuffer)))
		lba.SetLogBuffer(t.logBuffer)
	}
}

// Summary waits for all events to be processed, then shows the final summary.
// Call this after sending all events and closing the channel.
// Restores the original slog logger (not thread-safe, see interceptSlog).
func (t *Tracker[T, E]) Summary(err error) {
	<-t.finished
	if t.visualizer != nil {
		t.visualizer.Summary(err)
	}
	if t.previousLogger != nil {
		slog.SetDefault(t.previousLogger)
	}
}
