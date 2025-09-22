package sync

import (
	"cmp"
	"context"
	"errors"
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
	// If true, the graph is reversed before processing.
	//
	// Effectively, that means that a vertex is only processed when all its children
	// have been processed.
	Reverse bool
}

type ProcessTopologyOption func(*ProcessTopologyOptions)

func WithProcessGoRoutineLimit(limit int) ProcessTopologyOption {
	return func(o *ProcessTopologyOptions) {
		o.GoRoutineLimit = limit
	}
}

func WithReverseTopology() ProcessTopologyOption {
	return func(o *ProcessTopologyOptions) {
		o.Reverse = true
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
) (retErr error) {
	defer func() {
		if r := recover(); r != nil {
			retErr = errors.Join(retErr, fmt.Errorf("discovery panicked: %v", r))
		}
	}()
	if d.LengthVertices() == 0 {
		return nil
	}
	options := &ProcessTopologyOptions{}
	for _, opt := range opts {
		opt(options)
	}
	if options.GoRoutineLimit <= 0 {
		options.GoRoutineLimit = runtime.NumCPU()
	}
	var err error
	// Clone the graph to avoid modifying the original one.
	// We need to modify the graph during processing (removing edges and
	// vertices).
	topology := d.Clone()
	if options.Reverse {
		topology, err = topology.Reverse()
		if err != nil {
			return fmt.Errorf("failed to reverse graph: %w", err)
		}
	}

	// Collect all Children nodes that are end leafs
	roots := topology.Roots()
	for _, r := range roots {
		d.MustGetVertex(r).Attributes.Store(AttributeProcessingState, ProcessingStateQueued)
	}

	// A map to track doneMap nodes
	doneMap := &sync.Map{}

	// Process nodes concurrently
	if err := d.processTopology(ctx, topology, roots, processor, doneMap, options); err != nil {
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
	topology *DirectedAcyclicGraph[T],
	ids []T, // a list of root nodes to start processing with
	processor VertexProcessor[T], // the processing function
	doneMap *sync.Map, // a map to track loaded nodes
	opts *ProcessTopologyOptions,
) error {
	next, err := d.processLayer(ctx, topology, ids, processor, doneMap, opts)
	if err != nil {
		return err
	}

	// Recursively process the next batch if available.
	if len(next) == 0 {
		return nil
	}

	if err := d.processTopology(ctx, topology, next, processor, doneMap, opts); err != nil {
		return fmt.Errorf("failed to process topology: %w", err)
	}
	return nil
}

// processLayer concurrently calls the processor for each id in ids.
// Each id in ids is supposed to be a vertex whose parents have all been processed.
// After processing all ids, it collects all parents of the processed ids whose
// children have all been processed and returns them for the next round of processing.
func (d *DirectedAcyclicGraph[T]) processLayer(
	ctx context.Context,
	topology *DirectedAcyclicGraph[T],
	ids []T,
	processor VertexProcessor[T],
	doneMap *sync.Map,
	opts *ProcessTopologyOptions,
) ([]T, error) {
	errGroup, ctx := errgroup.WithContext(ctx)
	errGroup.SetLimit(opts.GoRoutineLimit)

	// wrap this logic into a function to ensure the nextQueueCh is closed
	for _, id := range ids {
		errGroup.Go(func() error {
			// Mark the id as processed or return if already processed.
			if _, loaded := doneMap.LoadOrStore(id, true); loaded {
				return nil
			}

			// Adding the attribute is done on the original dag (d) instead
			// of on the topology - which is a clone of d - that is modified
			// during ProcessTopology to perform the topological traversal.
			d.MustGetVertex(id).Attributes.Store(AttributeProcessingState, ProcessingStateProcessing)
			if err := processor.ProcessVertex(ctx, id); err != nil {
				d.MustGetVertex(id).Attributes.Store(AttributeProcessingState, ProcessingStateError)
				return fmt.Errorf("failed to process vertex with id %v: %w", id, err)
			}
			d.MustGetVertex(id).Attributes.Store(AttributeProcessingState, ProcessingStateCompleted)
			return nil
		})
	}
	if err := errGroup.Wait(); err != nil {
		return nil, fmt.Errorf("failed to process vertices: %w", err)
	}

	// Calculate the upper bound for the next slice.
	// This determines how many ids can be processed concurrently in the next
	// phase
	upperBound := 0
	for _, id := range ids {
		upperBound += int(topology.MustGetOutDegree(id).Load())
	}
	next := make([]T, 0, upperBound)
	for _, id := range ids {
		vertex := topology.MustGetVertex(id)
		for _, child := range vertex.EdgeKeys() {
			inDegree := topology.MustGetInDegree(child)
			newValue := inDegree.Add(-1)
			// If all parents of the child have been processed and the
			// child has not been enqueued yet, add it to the next layer.
			if newValue == 0 {
				d.MustGetVertex(child).Attributes.Store(AttributeProcessingState, ProcessingStateQueued)
				next = append(next, child)
			}
			vertex.Edges.Delete(child)
		}
		topology.Vertices.Delete(id)
	}

	return next, nil
}
