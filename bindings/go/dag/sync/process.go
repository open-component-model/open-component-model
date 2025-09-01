package sync

import (
	"cmp"
	"context"
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"
)

// ProcessTopology performs topological sorting and transfers objects in order
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
	if err := topology.processObjectsByTopology(ctx, d, roots, processor, doneMap); err != nil {
		return err
	}

	if topology.LengthVertices() > 0 {
		return fmt.Errorf("failed to process all objects, remaining: %v", topology.Vertices)
	}

	return nil
}

// ProcessTopology performs topological sorting and transfers objects in order
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
	if err := topology.processObjectsByTopology(ctx, d, roots, processor, doneMap); err != nil {
		return err
	}

	if topology.LengthVertices() > 0 {
		return fmt.Errorf("failed to process all objects, remaining: %v", topology.Vertices)
	}

	return nil
}

type VertexProcessor[T cmp.Ordered] interface {
	ProcessVertex(ctx context.Context, vertex *Vertex[T]) error
}

type VertexProcessorFunc[T cmp.Ordered] func(ctx context.Context, vertex *Vertex[T]) error

func (f VertexProcessorFunc[T]) ProcessVertex(ctx context.Context, vertex *Vertex[T]) error {
	return f(ctx, vertex)
}

// processObjectsByTopology processes nodes in any topological order defined by the topology given through the graph.
// and handles dependencies
func (d *DirectedAcyclicGraph[T]) processObjectsByTopology(
	ctx context.Context,
	dag *DirectedAcyclicGraph[T],
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

			// While the traversal logic should operate on a copy of the graph
			// with a processing order specific topology, the processing of the
			// vertex should be done on the original graph.
			vertex := dag.MustGetVertex(id)

			if err := processor.ProcessVertex(ctx, vertex); err != nil {
				return fmt.Errorf("failed to process vertex with id %v: %w", vertex.ID, err)
			}

			vertex = d.MustGetVertex(id)
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
		if err := d.processObjectsByTopology(ctx, dag, next, processor, doneMap); err != nil {
			return err
		}
	}

	return nil
}
