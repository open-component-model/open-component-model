package sync

import (
	"cmp"
	"context"
	"fmt"
	"runtime"
	"sync"

	"golang.org/x/sync/errgroup"
)

// ProcessingState is an attribute set during ProcessTopology and
// ProcessReverseTopology on each vertex to indicate its processing state
type ProcessingState int

func (s ProcessingState) String() string {
	switch s {
	case ProcessingStateQueued:
		return "queued"
	case ProcessingStateProcessing:
		return "processing"
	case ProcessingStateCompleted:
		return "completed"
	case ProcessingStateError:
		return "error"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

const (
	AttributeProcessingState = "dag/processing-state"

	// ProcessingStateQueued indicates the vertex has been queued for processing.
	// So, all its parents have been processed.
	ProcessingStateQueued = iota
	// ProcessingStateProcessing indicates the vertex is currently being processed.
	ProcessingStateProcessing
	// ProcessingStateCompleted indicates the vertex has been processed.
	ProcessingStateCompleted
	// ProcessingStateError indicates processing the vertex returned an error.
	ProcessingStateError
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
//
// ProcessVertex is guaranteed to be called for each vertex only once.
func (d *DirectedAcyclicGraph[T]) ProcessTopology(
	ctx context.Context,
	processor VertexProcessor[T],
	opts ...ProcessTopologyOption,
) error {
	options := &ProcessTopologyOptions{}
	for _, opt := range opts {
		opt(options)
	}
	if options.GoRoutineLimit <= 0 {
		options.GoRoutineLimit = runtime.NumCPU()
	}

	if d.LengthVertices() == 0 {
		return nil
	}
	topology := d.Clone()

	// Collect all Children nodes that are end leafs
	roots := topology.Roots()
	for _, r := range roots {
		d.MustGetVertex(r).Attributes.Store(AttributeProcessingState, ProcessingStateQueued)
	}

	// A map to track doneMap nodes
	doneMap := &sync.Map{}

	// Process nodes concurrently
	if err := topology.processTopology(ctx, d, roots, processor, doneMap, options); err != nil {
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
// ProcessVertex is guaranteed to be called for each vertex only once.
//
// For a more thorough explanation of topological order, see ProcessTopology.
func (d *DirectedAcyclicGraph[T]) ProcessReverseTopology(
	ctx context.Context,
	processor VertexProcessor[T],
	opts ...ProcessTopologyOption,
) error {
	options := &ProcessTopologyOptions{}
	for _, opt := range opts {
		opt(options)
	}
	if options.GoRoutineLimit <= 0 {
		options.GoRoutineLimit = runtime.NumCPU()
	}

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
	for _, r := range roots {
		d.MustGetVertex(r).Attributes.Store(AttributeProcessingState, ProcessingStateQueued)
	}

	// A map to track doneMap nodes
	doneMap := &sync.Map{}

	// Process nodes concurrently
	if err := topology.processTopology(ctx, d, roots, processor, doneMap, options); err != nil {
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
	dag *DirectedAcyclicGraph[T],
	ids []T, // a list of root nodes to start processing with
	processor VertexProcessor[T], // the processing function
	doneMap *sync.Map, // a map to track loaded nodes
	opts *ProcessTopologyOptions,
) error {
	errGroup, ctx := errgroup.WithContext(ctx)
	errGroup.SetLimit(opts.GoRoutineLimit)
	// Calculate the upper bound for the next queue channel
	// this determines how many ids can be processed concurrently in the next
	// phase
	upperBound := 0
	for _, id := range ids {
		upperBound += d.MustGetOutDegree(id)
	}

	nextQueueCh := make(chan T, upperBound)

	for _, id := range ids {
		errGroup.Go(func() error {
			// Mark the id as processed or return if already processed.
			_, loaded := doneMap.LoadOrStore(id, true)
			if loaded {
				return nil
			}

			dag.MustGetVertex(id).Attributes.Store(AttributeProcessingState, ProcessingStateProcessing)
			if err := processor.ProcessVertex(ctx, id); err != nil {
				dag.MustGetVertex(id).Attributes.Store(AttributeProcessingState, ProcessingStateError)
				return fmt.Errorf("failed to process vertex with id %v: %w", id, err)
			}
			dag.MustGetVertex(id).Attributes.Store(AttributeProcessingState, ProcessingStateCompleted)

			vertex := d.MustGetVertex(id)
			for _, parent := range vertex.EdgeKeys() {
				inDegree := d.MustGetInDegree(parent)
				d.InDegree.Store(parent, inDegree-1)
				parentVertex := d.MustGetVertex(parent)
				parentVertex.Edges.Delete(id)
				// If all prerequisites (children) of the parent have been
				// processed and the parent has not been enqueued yet, add it to
				// the next level.
				if d.MustGetInDegree(parent) == 0 {
					dag.MustGetVertex(parent).Attributes.Store(AttributeProcessingState, ProcessingStateQueued)
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
		if err := d.processTopology(ctx, dag, next, processor, doneMap, opts); err != nil {
			return err
		}
	}

	return nil
}
