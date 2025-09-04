package sync

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	// has not yet been processed by DiscoverNeighbors (direct neighbors are not
	// known yet).
	StateDiscovering TraversalState = iota
	// StateDiscovered indicates the vertex has been processed by the
	// DiscoverNeighbors, but its neighbors or transitive neighbors have not all
	// been processed by DiscoverNeighbors yet.
	StateDiscovered
	// StateCompleted indicates the vertex and all its neighbors have been
	// processed by the DiscoverNeighbors (sub-graph up to this vertex is fully
	// completed).
	StateCompleted
	// StateError indicates DiscoverNeighbors returned an error for this vertex
	// or a neighbor.
	StateError
)

// TODO(fabianburth): Add a recursion depth limit
type TraverseOptions[T cmp.Ordered] struct {
	// Roots to start the traversal from
	Roots          []T
	GoRoutineLimit int
}

type TraverseOption[T cmp.Ordered] func(*TraverseOptions[T])

func WithGoRoutineLimit[T cmp.Ordered](numGoRoutines int) TraverseOption[T] {
	return func(options *TraverseOptions[T]) {
		options.GoRoutineLimit = numGoRoutines
	}
}

func WithRoots[T cmp.Ordered](root ...T) TraverseOption[T] {
	return func(options *TraverseOptions[T]) {
		options.Roots = root
	}
}

// NeighborDiscoverer is an interface for a function that discovers neighbor
// vertices for a given vertex.
// It MUST treat v as read-only and return its neighbors id
// or an error.
type NeighborDiscoverer[T cmp.Ordered] interface {
	// DiscoverNeighbors is not allowed to set attributes on the neighbor
	// vertices as this would lead to data races. Those data races would occur
	// when multiple vertices have the same neighbor.
	//    A
	//   / \
	//  B   C
	//   \ /
	//    D
	// In this case, B and C could overwrite each others attributes on D.
	DiscoverNeighbors(ctx context.Context, v T) (neighbors []T, err error)
}

// DiscoverNeighborsFunc is a function type that implements the NeighborDiscoverer
// interface. It is used to discover neighbors for a given vertex.
type DiscoverNeighborsFunc[T cmp.Ordered] func(ctx context.Context, v T) (neighbors []T, err error)

func (f DiscoverNeighborsFunc[T]) DiscoverNeighbors(ctx context.Context, v T) (neighbors []T, err error) {
	return f(ctx, v)
}

// Traverse performs a concurrent depth-first traversal from the given root vertex.
// For each vertex v, it calls discoverer.DiscoverNeighbors(v), which MUST treat
// v as read-only and return its neighbors (created via NewVertex) or an error.
// The new vertices returned MUST not contain any edges, as DiscoverNeighbors
// will be called for them individually.
// DiscoverNeighbors is guaranteed to be called for each vertex only once.
//
// Returned neighbors need no pre-set edges but may include an
// AttributeOrderIndex and other business logic related attributes which can be
// interpreted by other tools.
//
// Traverse tracks each vertex’s TraversalState attribute and halts on error.
// See TraversalState for more details.
func (d *DirectedAcyclicGraph[T]) Traverse(
	ctx context.Context,
	discoverer NeighborDiscoverer[T],
	opts ...TraverseOption[T],
) error {
	options := &TraverseOptions[T]{}
	for _, opt := range opts {
		opt(options)
	}

	if options.GoRoutineLimit <= 0 {
		options.GoRoutineLimit = runtime.NumCPU()
	}
	if len(options.Roots) > 0 {
		for _, root := range options.Roots {
			if err := d.AddVertex(root); err != nil && !errors.Is(err, ErrAlreadyExists) {
				return fmt.Errorf("failed to add vertex for rootID %v: %w", root, err)
			}
		}
	} else {
		slog.DebugContext(ctx, "no roots provided for traversal, using dag roots")

		options.Roots = d.Roots()
		if len(options.Roots) == 0 {
			return fmt.Errorf("no roots provided and no roots found in the dag, cannot traverse")
		}
	}
	doneMap := &sync.Map{}
	errGroup := errgroup.Group{}

	for _, root := range options.Roots {
		// We ensured that the rootID vertex exists in the graph
		v, _ := d.GetVertex(root)
		v.Attributes.Store(AttributeTraversalState, StateDiscovering)
		// Traverse the graph from each rootID vertex concurrently.
		// This is fine as:
		// - the doneMap ensures that each vertex is only processed once.
		errGroup.Go(func() error {
			return d.traverse(ctx, root, discoverer, doneMap, options)
		})
	}
	if err := errGroup.Wait(); err != nil {
		return fmt.Errorf("failed to traverse graph: %w", err)
	}
	return nil
}

func (d *DirectedAcyclicGraph[T]) traverse(
	ctx context.Context,
	id T,
	discoverer NeighborDiscoverer[T],
	doneMap *sync.Map,
	opts *TraverseOptions[T],
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

	// Discover neighbors is not allowed to set attributes on the neighbor
	// vertices as this would lead to data races. Those data races would occur
	// when multiple vertices have the same neighbor.
	//    A
	//   / \
	//  B   C
	//   \ /
	//    D
	// In this case, B and C could overwrite each others attributes on D.
	//
	// Instead, DiscoverNeighbors is only allowed to return the IDs of the
	// If needed, we could introduce a capability to set attributes to the edges
	// map[neighborId]edgeAttributes in the future.
	neighbors, err := discoverer.DiscoverNeighbors(ctx, vertex.ID)
	if err != nil {
		vertex.Attributes.Store(AttributeTraversalState, StateError)
		return fmt.Errorf("failed to discoverer id %v: %w", id, err)
	}
	vertex.Attributes.Store(AttributeTraversalState, StateDiscovered)

	errGroup, ctx := errgroup.WithContext(ctx)
	// TODO(fabianburth): Implement a worker pool approach.
	// This is already useful to enforce a sequential traversal
	// by setting the limit to 1. But in reality, this does not actually
	// limit the number of goroutines, as the traversal is recursive.
	errGroup.SetLimit(opts.GoRoutineLimit)

	for index, neighborID := range neighbors {
		if err := d.AddVertex(neighborID, map[string]any{
			AttributeTraversalState: StateDiscovering,
		}); err != nil && !errors.Is(err, ErrAlreadyExists) {
			vertex.Attributes.Store(AttributeTraversalState, StateError)
			return fmt.Errorf("failed to add vertex for reference %v: %w", neighborID, err)
		}
		if err := d.AddEdge(id, neighborID, map[string]any{AttributeOrderIndex: index}); err != nil {
			vertex.Attributes.Store(AttributeTraversalState, StateError)
			return fmt.Errorf("failed to add edge %v: %w", id, err)
		}
		errGroup.Go(func() error {
			if err := d.traverse(ctx, neighborID, discoverer, doneMap, opts); err != nil {
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
