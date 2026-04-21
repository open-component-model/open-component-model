package progress

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"

	"golang.org/x/term"
)

// State represents the lifecycle state of a tracked item.
type State string

const (
	Running   State = "running"
	Completed State = "completed"
	Failed    State = "failed"
	Cancelled State = "cancelled"
	Unknown   State = "unknown"
)

// Event holds progress data for a single tracked item.
type Event[T any] struct {
	ID    string
	Name  string
	State State
	Err   error
	Data  T
}

// Operation represents a running unit of work created by [Tracker.StartOperation].
type Operation interface {
	Finish(err error)
}

// OperationOption configures an operation created by [Tracker.StartOperation].
type OperationOption[T any] func(*operationConfig[T])

type operationConfig[T any] struct {
	total          int
	errorFormatter func(T, error) string
	events         <-chan Event[T]
}

// WithEvents sets the event source for an operation.
// The mapper converts raw events (E) to typed [Event]s.
// Event processing is started by [Tracker.StartOperation].
func WithEvents[T, E any](events <-chan E, mapper func(E) Event[T], total int) OperationOption[T] {
	return func(cfg *operationConfig[T]) {
		cfg.total = total
		cfg.events = mapChannel(events, mapper)
	}
}

// mapChannel converts a typed channel to an Event[T] channel.
func mapChannel[T, E any](in <-chan E, mapper func(E) Event[T]) <-chan Event[T] {
	out := make(chan Event[T])
	go func() {
		defer close(out)
		for e := range in {
			out <- mapper(e)
		}
	}()
	return out
}

// WithErrorFormatter sets a typed error formatter for the operation.
func WithErrorFormatter[T any](f func(T, error) string) OperationOption[T] {
	return func(cfg *operationConfig[T]) {
		cfg.errorFormatter = f
	}
}

// Tracker manages progress rendering for CLI operations.
//
// In terminal mode, it uses the injected factory and redirects slog to a buffer
// so log output does not corrupt the animated UI.
// In non-terminal mode, a slog-based fallback is used.
type Tracker[T any] struct {
	out            io.Writer
	isTerminal     bool
	previousLogger *slog.Logger
	logBuffer      *SyncBuffer
	factory        VisualizerFactory[T]
}

// NewTracker creates a tracker for the given output writer.
// The factory creates terminal visualizers; in non-terminal mode it is ignored.
// If factory is nil, the tracker always uses [SlogVisualizer].
func NewTracker[T any](_ context.Context, out io.Writer, factory ...VisualizerFactory[T]) *Tracker[T] {
	var f VisualizerFactory[T]
	if len(factory) > 0 {
		f = factory[0]
	}
	return &Tracker[T]{
		out:        out,
		isTerminal: f != nil && isTerminal(out),
		factory:    f,
	}
}

// StartOperation creates and starts an operation with the given name.
func (t *Tracker[T]) StartOperation(name string, opts ...OperationOption[T]) Operation {
	cfg := &operationConfig[T]{}
	for _, opt := range opts {
		opt(cfg)
	}

	var vis Visualizer[T]
	if t.isTerminal {
		vis = t.factory(t.out, cfg.total)
		if cfg.errorFormatter != nil {
			if setter, ok := vis.(ErrorFormatterSetter[T]); ok {
				setter.SetErrorFormatter(cfg.errorFormatter)
			}
		}
		t.interceptSlog(vis)
	} else {
		vis = &SlogVisualizer[T]{}
	}

	vis.Begin(name)

	op := &operation[T]{vis: vis}
	if cfg.events != nil {
		op.processEvents(cfg.events)
	}

	return op
}

// interceptSlog redirects slog to a buffer and passes it to the visualizer
// if it supports [LogBufferAware].
func (t *Tracker[T]) interceptSlog(vis Visualizer[T]) {
	lba, ok := vis.(LogBufferAware)
	if !ok {
		return
	}
	if t.previousLogger == nil {
		t.previousLogger = slog.Default()
	}
	t.logBuffer = &SyncBuffer{}
	slog.SetDefault(slog.New(newBufferedHandler(t.logBuffer, t.previousLogger.Handler())))
	lba.SetLogBuffer(t.logBuffer)
}

func isTerminal(out io.Writer) bool {
	if f, ok := out.(*os.File); ok {
		return term.IsTerminal(int(f.Fd())) //nolint:gosec // G115
	}
	return false
}

// Stop restores the original slog logger. Must be called when done.
// Safe to call on a nil receiver and multiple times.
func (t *Tracker[T]) Stop() {
	if t == nil {
		return
	}
	if t.previousLogger != nil {
		slog.SetDefault(t.previousLogger)
		t.previousLogger = nil
	}
}

type operation[T any] struct {
	vis      Visualizer[T]
	finished chan struct{}
}

func (op *operation[T]) Finish(err error) {
	if op.finished != nil {
		<-op.finished
	}
	op.vis.End(err)
}

func (op *operation[T]) processEvents(events <-chan Event[T]) {
	op.finished = make(chan struct{})
	go func() {
		defer close(op.finished)
		for event := range events {
			if event.Err != nil && (errors.Is(event.Err, context.Canceled) || errors.Is(event.Err, context.DeadlineExceeded)) {
				event.State = Cancelled
				event.Err = nil
			}
			op.vis.HandleEvent(event)
		}
	}()
}
