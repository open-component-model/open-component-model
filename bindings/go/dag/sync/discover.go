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

// DiscoveryState is an attribute set during Discover()
// on each vertex to indicate its discovery state
type DiscoveryState int

func (s DiscoveryState) String() string {
	switch s {
	case DiscoveryStateDiscovering:
		return "discovering"
	case DiscoveryStateDiscovered:
		return "discovered"
	case DiscoveryStateCompleted:
		return "completed"
	case DiscoveryStateError:
		return "error"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

const (
	AttributeDiscoveryState = "dag/discovery-state"
	AttributeOrderIndex     = "dag/order-index"

	// DiscoveryStateDiscovering indicates the vertex has been added to the graph, but it
	// has not yet been processed by DiscoverNeighbors (direct neighbors are not
	// known yet).
	DiscoveryStateDiscovering DiscoveryState = iota
	// DiscoveryStateDiscovered indicates the vertex has been processed by the
	// DiscoverNeighbors, but its neighbors or transitive neighbors have not all
	// been processed by DiscoverNeighbors yet.
	DiscoveryStateDiscovered
	// DiscoveryStateCompleted indicates the vertex and all its neighbors have been
	// processed by the DiscoverNeighbors (sub-graph up to this vertex is fully
	// completed).
	DiscoveryStateCompleted
	// DiscoveryStateError indicates DiscoverNeighbors returned an error for this vertex
	// or a neighbor.
	DiscoveryStateError
)

// TODO(fabianburth): Add a recursion depth limit
type DiscoverOptions[T cmp.Ordered] struct {
	// Roots to start the discovery from
	Roots          []T
	GoRoutineLimit int
}

type DiscoverOption[T cmp.Ordered] func(*DiscoverOptions[T])

func WithDiscoveryGoRoutineLimit[T cmp.Ordered](numGoRoutines int) DiscoverOption[T] {
	return func(options *DiscoverOptions[T]) {
		options.GoRoutineLimit = numGoRoutines
	}
}

func WithRoots[T cmp.Ordered](root ...T) DiscoverOption[T] {
	return func(options *DiscoverOptions[T]) {
		options.Roots = root
	}
}

// NeighborDiscoverer is an interface for a function that discovers neighbor
// vertices for a given vertex.
// It MUST treat v as read-only and returns its neighbors id
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

// Discover performs a concurrent depth-first discovery from the given root vertex.
// For each vertex v, it calls discoverer.DiscoverNeighbors(v), which
// return its neighbors in order or an error.
// DiscoverNeighbors is guaranteed to be called at most once for each vertex. If
// there is an error, the discovery stops - before having processed all vertices
// - and the error is returned.
//
// Discover tracks each vertex’s DiscoveryState attribute and halts on error.
// See DiscoveryState for more details.
func (d *DirectedAcyclicGraph[T]) Discover(
	ctx context.Context,
	discoverer NeighborDiscoverer[T],
	opts ...DiscoverOption[T],
) (retErr error) {
	defer func() {
		if r := recover(); r != nil {
			retErr = errors.Join(retErr, fmt.Errorf("discovery panicked: %v", r))
		}
	}()

	options := &DiscoverOptions[T]{}
	for _, opt := range opts {
		opt(options)
	}

	if options.GoRoutineLimit <= 0 {
		options.GoRoutineLimit = runtime.NumCPU()
	}

	if len(options.Roots) == 0 {
		slog.DebugContext(ctx, "no roots provided for discovery, using dag roots")
		if options.Roots = d.Roots(); len(options.Roots) == 0 {
			return fmt.Errorf("no roots provided and no roots found in the dag, cannot discover")
		}
	}

	for _, root := range options.Roots {
		if err := d.AddVertex(root); err != nil && !errors.Is(err, ErrAlreadyExists) {
			return fmt.Errorf("failed to add vertex for rootID %v: %w", root, err)
		}
	}

	doneMap := &sync.Map{}
	errGroup, errgroupCtx := errgroup.WithContext(ctx)
	errGroup.SetLimit(options.GoRoutineLimit)

	for _, root := range options.Roots {
		// We ensured that the rootID vertex exists in the graph
		v := d.MustGetVertex(root)
		v.Attributes.Store(AttributeDiscoveryState, DiscoveryStateDiscovering)
		// Discover the graph from each rootID vertex concurrently.
		// This is fine as:
		// - the doneMap ensures that each vertex is only processed once.
		errGroup.Go(func() error {
			return d.discover(errgroupCtx, root, discoverer, doneMap, options)
		})
	}
	if err := errGroup.Wait(); err != nil {
		return fmt.Errorf("failed to discover graph: %w", err)
	}
	return nil
}

func (d *DirectedAcyclicGraph[T]) discover(
	ctx context.Context,
	id T,
	discoverer NeighborDiscoverer[T],
	doneMap *sync.Map,
	opts *DiscoverOptions[T],
) error {
	// Check if the context is done before proceeding the discovery.
	// Without this check, there is no way to cancel the recursive discovery.
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// If there exists a done channel for the vertex, the vertex has already
	// been processed (done channel closed) or is currently being processed
	// (done channel open) by another goroutine.
	// Then, loaded is true.
	ch := make(chan struct{})
	// If we opened the done channel, we are also responsible for closing it.
	defer close(ch)

	doneCh, loaded := doneMap.LoadOrStore(id, ch)
	done := doneCh.(chan struct{})
	if loaded {
		// If the node is already being discovered, wait until its discovery is done.
		// Alternatively, if the context is cancelled early, abort.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return nil
		}
	}

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
		vertex.Attributes.Store(AttributeDiscoveryState, DiscoveryStateError)
		return fmt.Errorf("failed to discover id %v: %w", id, err)
	}
	vertex.Attributes.Store(AttributeDiscoveryState, DiscoveryStateDiscovered)

	errGroup, egctx := errgroup.WithContext(ctx)
	// TODO(fabianburth): Implement a worker pool approach.
	// This is already useful to enforce a sequential discovery
	// by setting the limit to 1. But in reality, this does not actually
	// limit the number of goroutines, as the discovery is recursive.
	errGroup.SetLimit(opts.GoRoutineLimit)

	for index, neighborID := range neighbors {
		// Add the discovered neighbors as a vertex to the graph. As multiple
		// vertices might have the same neighbor, ErrAlreadyExists is ignored.
		// 	    A
		//	   / \
		//	  B   C
		//	   \ /
		//	    D
		err := d.AddVertex(neighborID, map[string]any{
			AttributeDiscoveryState: DiscoveryStateDiscovering,
		})
		if err != nil && !errors.Is(err, ErrAlreadyExists) {
			vertex.Attributes.Store(AttributeDiscoveryState, DiscoveryStateError)
			return fmt.Errorf("failed to add vertex for reference %v: %w", neighborID, err)
		}
		if err := d.AddEdge(id, neighborID, map[string]any{AttributeOrderIndex: index}); err != nil {
			vertex.Attributes.Store(AttributeDiscoveryState, DiscoveryStateError)
			return fmt.Errorf("failed to add edge from %v to %v: %w", id, neighborID, err)
		}
		if errors.Is(err, ErrAlreadyExists) {
			continue
		}
		errGroup.Go(func() error {
			if err := d.discover(egctx, neighborID, discoverer, doneMap, opts); err != nil {
				return fmt.Errorf("failed to discover reference %v: %w", neighborID, err)
			}
			return nil
		})
	}
	if err = errGroup.Wait(); err != nil {
		vertex.Attributes.Store(AttributeDiscoveryState, DiscoveryStateError)
		return err
	}
	vertex.Attributes.Store(AttributeDiscoveryState, DiscoveryStateCompleted)
	return nil
}
