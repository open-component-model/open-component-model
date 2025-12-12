package sync

import (
	"cmp"
	"context"
)

// GraphProcessorOptions configures the behavior of a GraphProcessor.
type GraphProcessorOptions[K cmp.Ordered, V any] struct {
	// Processor what is called for each node value during processing.
	Processor Processor[V]
	// Concurrency limits the number of concurrent processing goroutines.
	// If <= 0, concurrency is unlimited.
	Concurrency int
}

// Processor defines the interface required to process node values
// during traversal.
type Processor[V any] interface {
	// ProcessValue is called for each node value during traversal.
	// It must be safe to call concurrently.
	// Every node value is guaranteed to be processed at most once.
	// It MUST return nil on success, or an error on failure.
	ProcessValue(ctx context.Context, value V) error
}

// ProcessorFunc is an adapter to allow ordinary functions to act as a Processor.
type ProcessorFunc[V any] func(ctx context.Context, value V) error

// ProcessValue calls the underlying function.
func (f ProcessorFunc[V]) ProcessValue(ctx context.Context, value V) error {
	return f(ctx, value)
}
