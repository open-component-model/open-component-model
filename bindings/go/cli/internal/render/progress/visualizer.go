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

// IndeterminateTotal serves as the total for [WithEvents] when the item count is not
// known up front, for example during recursive discovery where new items appear while
// the operation runs. Visualizers show the item log but no progress bar in that case.
const IndeterminateTotal = -1

// ErrorFormatterSetter is an optional interface for visualizers that accept an error formatter.
type ErrorFormatterSetter[T any] interface {
	SetErrorFormatter(f func(T, error) string)
}

// ConcurrencyAware is an optional interface for visualizers that can report the
// configured parallelism of an operation, i.e. how many items ("runners") may
// be processed at the same time. Set via [WithConcurrency].
type ConcurrencyAware interface {
	SetConcurrency(runners int)
}
