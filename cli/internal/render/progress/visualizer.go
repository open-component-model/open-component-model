package progress

import "io"

// Visualizer displays progress for an operation.
type Visualizer[T any] interface {
	Begin(name string)
	HandleEvent(event Event[T])
	End(err error)
}

// VisualizerFactory creates a Visualizer for the given output and total item count.
// For simple operations (no events), total is 0.
type VisualizerFactory[T any] func(out io.Writer, total int) Visualizer[T]

// ErrorFormatterSetter is an optional interface for visualizers that accept an error formatter.
type ErrorFormatterSetter[T any] interface {
	SetErrorFormatter(f func(T, error) string)
}
