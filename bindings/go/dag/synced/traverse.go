package synced

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"

	"golang.org/x/sync/errgroup"
)

type DiscoveryState int

const (
	AttributeDiscoveryState = "dag/discovery-state"
	AttributeOrderIndex     = "dag/order-index"

	StateDiscovering DiscoveryState = iota
	StateDiscovered
	StateCompleted
	StateError
)

// TODO(fabianburth): Add a recursion depth limit
type TraverseOptions struct {
	GoRoutineLimit int
}

type TraverseOption func(*TraverseOptions)

func WithGoRoutineLimit(numGoRoutines int) TraverseOption {
	return func(options *TraverseOptions) {
		options.GoRoutineLimit = numGoRoutines
	}
}

// Traverse performs a concurrent depth-first search (dfs). Therefore, it
// recursively discovers the graph from the root vertex using the
// provided traversalFunc function.
//
// Thereby, it sets a DiscoveryState attribute
// on each vertex to indicate its discovery state:
// - StateDiscovering: The vertex has been added to the graph, but it has not yet
// been processed by the traversalFunc (direct neighbors are not known yet).
// - StateDiscovered: The vertex has been processed by the traversalFunc, but
// its neighbors or transitive neighbors have not all been processed by the
// traversalFunc yet.
// - StateCompleted: The vertex and all its neighbors have been processed by the
// traversalFunc (sub-graph up to this vertex is fully completed).
// - StateError: The traversalFunc returned an error for this vertex, indicating
// that the traversal could not be completed for this vertex.
//
// The traversalFunc is called for each vertex - starting with the root vertex.
// It has access to the vertex and all its attributes. The traversalFunc SHOULD
// treat the vertex v as READ-ONLY and SHOULD NOT modify the edges of vertex v,
// as this may lead to undefined behavior.
// The traversalFunc returns a slice of neighbor vertices. The Traverse
// logic takes care of adding these neighbors to the graph and traversing them
// recursively (so, the edges on those vertices SHOULD NOT be set). Ideally, it
// uses NewVertex to create the neighbors.
// The traversalFunc can return an error, which will stop the traversal and set
// the discovery state of the vertex to StateError.
// The traversalFunc may set the AttributeOrderIndex attribute on the returned
// neighbors to indicate an order between the neighbors. This information is
// irrelevant for the traversal but may be interpreted by other tools.
func (d *DirectedAcyclicGraph[T]) Traverse(
	ctx context.Context,
	root *Vertex[T],
	traversalFunc func(ctx context.Context, v *Vertex[T]) (neighbors []*Vertex[T], err error),
	options ...TraverseOption,
) error {
	// Protect graph from concurrent execution of graph operations. Since
	// traverse is called recursively, this will lock until the entire traversal
	// is complete.
	d.mu.Lock()
	defer d.mu.Unlock()

	opts := &TraverseOptions{}
	for _, opt := range options {
		opt(opts)
	}

	if opts.GoRoutineLimit <= 0 {
		opts.GoRoutineLimit = runtime.NumCPU()
	}
	if err := d.AddVertex(root, map[string]any{
		AttributeDiscoveryState: StateDiscovering,
	}); err != nil && !errors.Is(err, ErrAlreadyExists) {
		return fmt.Errorf("failed to add vertex for rootID %v: %w", root, err)
	}
	return d.traverse(ctx, root.ID, traversalFunc, &sync.Map{}, opts)
}

func (d *DirectedAcyclicGraph[T]) traverse(
	ctx context.Context,
	id T,
	process func(ctx context.Context, v *Vertex[T]) (neighbors []*Vertex[T], err error),
	doneMap *sync.Map,
	opts *TraverseOptions,
) error {
	// Check if the context is done before proceeding the traversal.
	// Without this check, there is no way to cancel the recursive traversal.
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// If there exists a done channel for the vertex, the vertex has already
	// been processed (done channel closed) or is currently being processed
	// (done channel open) by another goroutine.
	// Then, loaded is true.
	doneCh, loaded := doneMap.LoadOrStore(id, make(chan struct{}))
	done := doneCh.(chan struct{})
	if loaded {
		// If the node is already being discovered, wait until its discovery is done.
		// Alternatively, if the context is cancelled early, abort.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
		}
		return nil
	}
	// If we opened the done channel, we are also responsible for closing it.
	defer close(done)

	vertex, ok := d.GetVertex(id)
	if !ok {
		return fmt.Errorf("vertex %v not found in the graph", id)
	}

	neighbors, err := process(ctx, vertex)
	if err != nil {
		vertex.Attributes.Store(AttributeDiscoveryState, StateError)
		return fmt.Errorf("failed to process id %v: %w", id, err)
	}
	vertex.Attributes.Store(AttributeDiscoveryState, StateDiscovered)

	errGroup, ctx := errgroup.WithContext(ctx)
	// TODO(fabianburth): Implement a worker pool approach.
	// This is already useful to enforce a sequential traversal
	// by setting the limit to 1. But in reality, this does not actually
	// limit the number of goroutines, as the traversal is recursive.
	errGroup.SetLimit(opts.GoRoutineLimit)

	for index, ref := range neighbors {
		if err := d.AddVertex(ref, map[string]any{
			AttributeDiscoveryState: StateDiscovering},
		); err != nil && !errors.Is(err, ErrAlreadyExists) {
			vertex.Attributes.Store(AttributeDiscoveryState, StateError)
			return fmt.Errorf("failed to add vertex for reference %v: %w", ref, err)
		}
		if err := d.AddEdge(id, ref.ID, map[string]any{AttributeOrderIndex: index}); err != nil {
			vertex.Attributes.Store(AttributeDiscoveryState, StateError)
			return fmt.Errorf("failed to add edge %v: %w", id, err)
		}
		refID := ref.ID
		errGroup.Go(func() error {
			if err := d.traverse(ctx, refID, process, doneMap, opts); err != nil {
				return fmt.Errorf("failed to traverse reference %v: %w", id, err)
			}
			return nil
		})
	}
	if err = errGroup.Wait(); err != nil {
		vertex.Attributes.Store(AttributeDiscoveryState, StateError)
		return err
	}
	vertex.Attributes.Store(AttributeDiscoveryState, StateCompleted)
	return nil
}
