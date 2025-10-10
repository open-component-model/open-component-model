package sync

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/dag"
)

// DiscoveryState represents the lifecycle stage of a vertex during discovery.
// This allows consumers to track progress, completion, and errors per vertex.
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

// Attribute keys stored on each vertex.
const (
	AttributeValue          = "dag/value"           // resolved value for the vertex
	AttributeDiscoveryState = "dag/discovery-state" // discovery state
	AttributeOrderIndex     = "dag/order-index"     // order of neighbor edge
)

// Discovery states describe how far processing has gone for each vertex.
const (
	// DiscoveryStateUnknown means the vertex has not been processed yet or is in an unknown state.
	DiscoveryStateUnknown DiscoveryState = iota
	// DiscoveryStateDiscovering means the vertex is being resolved.
	DiscoveryStateDiscovering
	// DiscoveryStateDiscovered means the vertex was resolved but neighbors were not yet fully explored.
	DiscoveryStateDiscovered
	// DiscoveryStateCompleted means the vertex and all its reachable neighbors successfully discovered.
	DiscoveryStateCompleted
	// DiscoveryStateError means discovery failed for this vertex or one of its neighbors.
	DiscoveryStateError
)

// NewGraphDiscoverer constructs a discoverer instance with its own
// concurrency-safe DAG and per-vertex "done" channels.
func NewGraphDiscoverer[K cmp.Ordered, V any](opts *GraphDiscovererOptions[K, V]) *GraphDiscoverer[K, V] {
	return &GraphDiscoverer[K, V]{
		graph:   NewSyncedDirectedAcyclicGraph[K](),
		doneMap: &sync.Map{},
		opts:    opts,
	}
}

// GraphDiscoverer orchestrates concurrent graph discovery.
// - graph: the shared DAG being built
// - doneMap: ensures each vertex is processed exactly once
// - opts: external resolver and neighbor discoverer
type GraphDiscoverer[K cmp.Ordered, V any] struct {
	opts    *GraphDiscovererOptions[K, V]
	graph   *SyncedDirectedAcyclicGraph[K]
	doneMap *sync.Map
}

func (d *GraphDiscoverer[K, V]) Graph() *SyncedDirectedAcyclicGraph[K] {
	return d.graph
}

// CurrentEdges returns the ordered neighbors of a vertex from the DAG snapshot.
func (d *GraphDiscoverer[K, V]) CurrentEdges(key K) []K {
	var edges []K
	_ = d.graph.WithReadLock(func(d *dag.DirectedAcyclicGraph[K]) error {
		v, ok := d.Vertices[key]
		if !ok {
			return nil
		}
		edges = make([]K, len(v.Edges))
		for k, edge := range v.Edges {
			// Order edges by index if present.
			// If not, we append them in the order they are returned from the iterator (not stable).
			if idx, ok := edge[AttributeOrderIndex]; ok {
				edges[idx.(int)] = k
			} else {
				panic("edges without order index should never exist")
			}
		}
		return nil
	})
	return edges
}

// CurrentValue returns the resolved value of a vertex, or zero if absent.
func (d *GraphDiscoverer[K, V]) CurrentValue(key K) V {
	var value V
	_ = d.graph.WithReadLock(func(d *dag.DirectedAcyclicGraph[K]) error {
		v, ok := d.Vertices[key]
		if !ok {
			return nil
		}
		value, _ = v.Attributes[AttributeValue].(V)
		return nil
	})
	return value
}

// CurrentState returns the discovery state of a vertex.
func (d *GraphDiscoverer[K, V]) CurrentState(key K) DiscoveryState {
	state := DiscoveryStateUnknown
	_ = d.graph.WithReadLock(func(d *dag.DirectedAcyclicGraph[K]) error {
		v, ok := d.Vertices[key]
		if !ok {
			// if the vertex does not exist, we consider its state unknown
			// this may mean that it simply has not been discovered yet.
			state = DiscoveryStateUnknown
			return nil
		}
		s, ok := v.Attributes[AttributeDiscoveryState]
		if !ok {
			panic("vertex without discovery state should never exist")
		}
		state = s.(DiscoveryState)
		return nil
	})

	return state
}

// Discover performs concurrent recursive discovery starting from the given roots.
// Guarantees:
// - Each vertex is resolved at most once.
// - If any error occurs, discovery halts and propagates the error.
// - States are updated consistently on success or failure.
func (d *GraphDiscoverer[K, V]) Discover(ctx context.Context) (retErr error) {
	// Ensure panic safety even with concurrency in place.
	defer func() {
		if r := recover(); r != nil {
			retErr = errors.Join(retErr, fmt.Errorf("discovery panicked: %v", r))
		}
	}()

	if len(d.opts.Roots) == 0 {
		return fmt.Errorf("no roots provided and no roots found in the dag, cannot discover")
	}

	errGroup, errgroupCtx := errgroup.WithContext(ctx)
	for _, root := range d.opts.Roots {
		errGroup.Go(func() error {
			return d.discover(errgroupCtx, root)
		})
	}

	if err := errGroup.Wait(); err != nil {
		return fmt.Errorf("failed to discover graph: %w", err)
	}
	return nil
}

// discover resolves one vertex and recursively explores its neighbors.
func (d *GraphDiscoverer[K, V]) discover(
	ctx context.Context,
	id K,
) error {
	// Early abort if context is cancelled.
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Setup done channel for this vertex.
	ch := make(chan struct{})
	defer close(ch)

	// If already in progress, wait until it finishes.
	doneCh, loaded := d.doneMap.LoadOrStore(id, ch)
	done := doneCh.(chan struct{})
	if loaded {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return nil
		}
	}

	// Add vertex in "discovering" state.
	if err := d.graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[K]) error {
		return d.AddVertex(id, map[string]any{
			AttributeDiscoveryState: DiscoveryStateDiscovering,
		})
	}); err != nil {
		return err
	}

	// Resolve the vertex value.
	value, err := d.opts.Resolver.Resolve(ctx, id)

	// Update state after resolution.
	if err := d.graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[K]) error {
		if err != nil {
			d.Vertices[id].Attributes[AttributeDiscoveryState] = DiscoveryStateError
		} else {
			d.Vertices[id].Attributes[AttributeDiscoveryState] = DiscoveryStateDiscovered
			d.Vertices[id].Attributes[AttributeValue] = value
		}
		return err
	}); err != nil {
		return err
	}

	// Discover neighbors.
	neighbors, err := d.opts.Discoverer.Discover(ctx, value)

	// Update state after neighbor discovery.
	if err := d.graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[K]) error {
		if err != nil {
			d.Vertices[id].Attributes[AttributeDiscoveryState] = DiscoveryStateError
		} else {
			d.Vertices[id].Attributes[AttributeValue] = value
		}
		return err
	}); err != nil {
		return err
	}

	// Explore neighbors concurrently.
	errGroup, egctx := errgroup.WithContext(ctx)
	for index, neighbor := range neighbors {
		errGroup.Go(func() error {
			if err := d.discover(egctx, neighbor); err != nil {
				return fmt.Errorf("failed to discover reference %v: %w", neighbor, err)
			}
			// Add edge from current vertex to neighbor.
			return d.graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[K]) error {
				return d.AddEdge(id, neighbor, map[string]any{
					AttributeOrderIndex: index,
				})
			})
		})
	}
	err = errGroup.Wait()

	// Finalize state.
	return d.graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[K]) error {
		if err != nil {
			d.Vertices[id].Attributes[AttributeDiscoveryState] = DiscoveryStateError
		} else {
			d.Vertices[id].Attributes[AttributeDiscoveryState] = DiscoveryStateCompleted
		}
		return err
	})
}
