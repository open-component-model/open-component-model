package sync

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"

	"golang.org/x/sync/errgroup"
)

// TraversalState is an attribute set during Traverse()
// on each vertex to indicate its traversal state:
type TraversalState int

func (t TraversalState) String() string {
	switch t {
	case StateDiscovering:
		return "discovering"
	case StateDiscovered:
		return "discovered"
	case StateCompleted:
		return "completed"
	case StateError:
		return "error"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}

const (
	AttributeTraversalState = "dag/traversal-state"
	AttributeOrderIndex     = "dag/order-index"

	// StateDiscovering indicates the vertex has been added to the graph, but it
	// has not yet been processed by the traversalFunc (direct neighbors are not known yet).
	StateDiscovering TraversalState = iota
	// StateDiscovered indicates the vertex has been processed by the traversalFunc,
	// but its neighbors or transitive neighbors have not all been processed by the
	// traversalFunc yet.
	StateDiscovered
	// StateCompleted indicates the vertex and all its neighbors have been
	// processed by the traversalFunc (sub-graph up to this vertex is fully completed).
	StateCompleted
	// StateError indicates the traversalFunc returned an error for this vertex
	// or a neighbor.
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

// Traverse performs a concurrent depth-first traversal from the given root vertex.
// For each vertex v, it calls traversalFunc(v), which MUST treat v as read-only
// and return its neighbors (created via NewVertex) or an error. The new vertices
// returned MUST not contain any edges, as traversalFunc will be called for them
// individually.
//
// Returned neighbors need no pre-set edges but may include an
// AttributeOrderIndex and other business logic related attributes which can be
// interpreted by other tools.
//
// Traverse tracks each vertex’s TraversalState attribute and halts on error.
// See TraversalState for more details.
func (d *DirectedAcyclicGraph[T]) Traverse(
	ctx context.Context,
	root *Vertex[T],
	traversalFunc func(ctx context.Context, v *Vertex[T]) (neighbors []*Vertex[T], err error),
	options ...TraverseOption,
) error {
	opts := &TraverseOptions{}
	for _, opt := range options {
		opt(opts)
	}

	if opts.GoRoutineLimit <= 0 {
		opts.GoRoutineLimit = runtime.NumCPU()
	}
	if err := d.addRawVertex(root, map[string]any{
		AttributeTraversalState: StateDiscovering,
	}); err != nil && !errors.Is(err, ErrAlreadyExists) {
		return fmt.Errorf("failed to add vertex for rootID %v: %w", root, err)
	}
	return d.traverse(ctx, root.ID, traversalFunc, &sync.Map{}, opts)
}

func (d *DirectedAcyclicGraph[T]) traverse(
	ctx context.Context,
	id T,
	traversalFunc func(ctx context.Context, v *Vertex[T]) (neighbors []*Vertex[T], err error),
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

	neighbors, err := traversalFunc(ctx, vertex)
	if err != nil {
		vertex.Attributes.Store(AttributeTraversalState, StateError)
		return fmt.Errorf("failed to traversalFunc id %v: %w", id, err)
	}
	vertex.Attributes.Store(AttributeTraversalState, StateDiscovered)

	errGroup, ctx := errgroup.WithContext(ctx)
	// TODO(fabianburth): Implement a worker pool approach.
	// This is already useful to enforce a sequential traversal
	// by setting the limit to 1. But in reality, this does not actually
	// limit the number of goroutines, as the traversal is recursive.
	errGroup.SetLimit(opts.GoRoutineLimit)

	for index, ref := range neighbors {
		if err := d.addRawVertex(ref, map[string]any{
			AttributeTraversalState: StateDiscovering,
		}); err != nil && !errors.Is(err, ErrAlreadyExists) {
			vertex.Attributes.Store(AttributeTraversalState, StateError)
			return fmt.Errorf("failed to add vertex for reference %v: %w", ref, err)
		}
		if err := d.AddEdge(id, ref.ID, map[string]any{AttributeOrderIndex: index}); err != nil {
			vertex.Attributes.Store(AttributeTraversalState, StateError)
			return fmt.Errorf("failed to add edge %v: %w", id, err)
		}
		refID := ref.ID
		errGroup.Go(func() error {
			if err := d.traverse(ctx, refID, traversalFunc, doneMap, opts); err != nil {
				return fmt.Errorf("failed to traverse reference %v: %w", id, err)
			}
			return nil
		})
	}
	if err = errGroup.Wait(); err != nil {
		vertex.Attributes.Store(AttributeTraversalState, StateError)
		return err
	}
	vertex.Attributes.Store(AttributeTraversalState, StateCompleted)
	return nil
}
