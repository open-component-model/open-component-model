package sync

import (
	"cmp"
	"context"
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"
)

type ProcessTopologyOptions struct {
	// MaxGoroutines limits the number of concurrent goroutines processing
	// vertices. If 0, it defaults to the number of CPUs.
	GoRoutineLimit int
}

type ProcessTopologyOption func(*ProcessTopologyOptions)

func WithProcessGoRoutineLimit(limit int) ProcessTopologyOption {
	return func(o *ProcessTopologyOptions) {
		o.GoRoutineLimit = limit
	}
}

// ProcessTopology performs a traversal in topological order.
//
// Effectively, that means that a vertex is only processed when all its parents
// have been processed.
//
//	  A
//	 / \
//	B   C
//	 \ / \
//	  D   E
//
// In the above graph, A is a parent of B and C, and B and C are parents of D.
// The valid processing orders are for example:
// - A, B, C, D, E
// - A, C, B, D, E
// But not:
// - B, A, C, D, E (B before its parent A)
// - D, B, C, A, E (D before its parents B and C)
//
// The processing is done concurrently. In the above example, after A is
// processed, both B and C are processed concurrently. D and E will
// be processed only after both B and C have been processed - even though E is
// independent of B.
func (d *DirectedAcyclicGraph[T]) ProcessTopology(ctx context.Context, processor VertexProcessor[T]) error {
	if d.LengthVertices() == 0 {
		return nil
	}
	topology := d.Clone()

	// Collect all Children nodes that are end leafs
	roots := topology.Roots()

	// A map to track doneMap nodes
	doneMap := &sync.Map{}

	// Process nodes concurrently
	if err := topology.processTopology(ctx, roots, processor, doneMap); err != nil {
		return err
	}

	if topology.LengthVertices() > 0 {
		return fmt.Errorf("failed to process all objects, remaining: %v", topology.Vertices)
	}

	return nil
}

// ProcessReverseTopology reverses the graph (so, it inverts the direction of
// edges). Then it performs a traversal in topological order on the reversed
// graph.
//
// Effectively, that means that a vertex is only processed when all its children
// have been processed.
//
// For a more thorough explanation of topological order, see ProcessTopology.
func (d *DirectedAcyclicGraph[T]) ProcessReverseTopology(ctx context.Context, processor VertexProcessor[T]) error {
	if d.LengthVertices() == 0 {
		return nil
	}
	topology := d.Clone()
	topology, err := topology.Reverse()
	if err != nil {
		return fmt.Errorf("failed to reverse graph: %w", err)
	}

	// Collect all Children nodes that are end leafs
	roots := topology.Roots()

	// A map to track doneMap nodes
	doneMap := &sync.Map{}

	// Process nodes concurrently
	if err := topology.processTopology(ctx, roots, processor, doneMap); err != nil {
		return err
	}

	if topology.LengthVertices() > 0 {
		return fmt.Errorf("failed to process all objects, remaining: %v", topology.Vertices)
	}

	return nil
}

type VertexProcessor[T cmp.Ordered] interface {
	ProcessVertex(ctx context.Context, vertex T) error
}

type VertexProcessorFunc[T cmp.Ordered] func(ctx context.Context, vertex T) error

func (f VertexProcessorFunc[T]) ProcessVertex(ctx context.Context, vertex T) error {
	return f(ctx, vertex)
}

func (d *DirectedAcyclicGraph[T]) processTopology(
	ctx context.Context,
	ids []T, // a list of root nodes to start processing with
	processor VertexProcessor[T], // the processing function
	doneMap *sync.Map, // a map to track loaded nodes
) error {
	errGroup, ctx := errgroup.WithContext(ctx)

	// Calculate the upper bound for the next queue channel
	// this determines how many ids can be processed concurrently in the next phase
	upperBound := 0
	for _, id := range ids {
		upperBound += d.MustGetOutDegree(id)
	}

	nextQueueCh := make(chan T, upperBound)

	for _, id := range ids {
		errGroup.Go(func() error {
			// Mark the id as processed or return if already processed
			_, loaded := doneMap.LoadOrStore(id, true)
			if loaded {
				return nil
			}

			if err := processor.ProcessVertex(ctx, id); err != nil {
				return fmt.Errorf("failed to process vertex with id %v: %w", id, err)
			}

			vertex := d.MustGetVertex(id)
			for _, parent := range vertex.EdgeKeys() {
				inDegree := d.MustGetInDegree(parent)
				d.InDegree.Store(parent, inDegree-1)
				parentVertex := d.MustGetVertex(parent)
				parentVertex.Edges.Delete(id)
				// If all prerequisites (children) of the parent have been processed and
				// the parent has not been enqueued yet, add it to the next level.
				if d.MustGetInDegree(parent) == 0 {
					nextQueueCh <- parent
				}
			}
			d.Vertices.Delete(id)
			return nil
		})
	}

	if err := errGroup.Wait(); err != nil {
		return err
	}

	close(nextQueueCh)

	// Collect ids that are ready for the next round.
	var next []T
	for n := range nextQueueCh {
		next = append(next, n)
	}

	// Recursively process the next batch if available.
	if len(next) > 0 {
		if err := d.processTopology(ctx, next, processor, doneMap); err != nil {
			return err
		}
	}

	return nil
}
