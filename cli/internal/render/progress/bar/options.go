package bar

// VisualiserOption configures a barVisualizer using the functional options pattern.
type VisualiserOption[T any] func(visualizer *barVisualizer[T])

// WithHeader sets text shown above the progress display.
// Example: "Transferring component versions..."
func WithHeader[T any](header string) VisualiserOption[T] {
	return func(b *barVisualizer[T]) {
		b.header = header
	}
}

// WithNameFormatter sets how items are displayed in the progress log.
// Without this, items show their raw ID. With a formatter, you can show
// a human-friendly name like "component-name [TransformType]".
func WithNameFormatter[T any](f Formatter[T]) VisualiserOption[T] {
	return func(t *barVisualizer[T]) {
		t.formatter = f
	}
}

// WithErrorFormatter customizes how errors appear in the failure summary.
// Use this to include extra context like the transformation spec.
// Default: TreeErrorFormatter (shows error chain as indented tree).
func WithErrorFormatter[T any](f ErrorFormatter[T]) VisualiserOption[T] {
	return func(t *barVisualizer[T]) {
		t.errorFormatter = f
	}
}
