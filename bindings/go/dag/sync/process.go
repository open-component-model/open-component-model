// Package sync provides concurrency-safe utilities for processing directed
// acyclic graphs (DAGs). It supports concurrent, dependency-respecting graph
// traversal using Kahn's algorithm with a bounded worker pool.
package sync

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/dag"
)

// NewGraphProcessor creates a GraphProcessor for a concurrency-safe
// DAG with the given options. The returned processor will respect the provided
// concurrency settings and invoke the Processor for each node.
func NewGraphProcessor[K cmp.Ordered, V any](
	graph *SyncedDirectedAcyclicGraph[K],
	opts *GraphProcessorOptions[K, V],
) *GraphProcessor[K, V] {
	return &GraphProcessor[K, V]{graph: graph, opts: opts}
}

// GraphProcessor coordinates concurrent processing of a DAG.
// Nodes are processed only after all their predecessors have been processed.
type GraphProcessor[K cmp.Ordered, V any] struct {
	graph *SyncedDirectedAcyclicGraph[K]
	opts  *GraphProcessorOptions[K, V]
}

// CurrentValue retrieves the stored value for a node by key. If no value is set,
// the zero value of V is returned.
func (d *GraphProcessor[K, V]) CurrentValue(key K) V {
	var value V
	_ = d.graph.WithReadLock(func(g *dag.DirectedAcyclicGraph[K]) error {
		if v, ok := g.Vertices[key]; ok {
			value, _ = v.Attributes[AttributeValue].(V)
		}
		return nil
	})
	return value
}

// Process traverses the DAG in topological order and applies the configured
// Processor to each node. It implements a parallel form of Kahn's algorithm
// with a bounded worker pool:
//
//  1. Compute in-degrees for all vertices.
//  2. Schedule roots (in-degree == 0).
//  3. Whenever a node finishes, decrement its children's in-degrees and
//     schedule each child that reaches zero immediately.
//  4. Repeat until every node is processed or one fails.
//
// At most Concurrency nodes are processed at once (no limit if <= 0). There is
// no barrier between dependency levels: a node starts as soon as all of its
// own predecessors are done, regardless of unrelated slow nodes.
func (d *GraphProcessor[K, V]) Process(ctx context.Context) error {
	g := d.graph

	inDegree := make(map[K]int)
	var ready []K

	// Initialize in-degree map and root nodes.
	err := g.WithReadLock(func(g *dag.DirectedAcyclicGraph[K]) error {
		for id, v := range g.Vertices {
			inDegree[id] = v.InDegree
			if v.InDegree == 0 {
				ready = append(ready, id)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to read graph: %w", err)
	}

	total := len(inDegree)
	eg, gctx := errgroup.WithContext(ctx)
	if d.opts.Concurrency > 0 {
		eg.SetLimit(d.opts.Concurrency)
	}
	// Each node completes at most once, so sends never block. This matters:
	// the coordinator may be blocked in eg.Go while all workers report back.
	done := make(chan K, total)

	for finished := 0; finished < total; {
		for _, node := range ready {
			if gctx.Err() != nil {
				break
			}
			// Blocks while Concurrency nodes are in flight.
			eg.Go(func() error {
				if err := d.opts.Processor.ProcessValue(gctx, d.CurrentValue(node)); err != nil {
					return fmt.Errorf("failed to process value for node %v in graph: %w", node, err)
				}
				done <- node
				return nil
			})
		}
		ready = ready[:0]

		select {
		case node := <-done:
			finished++
			if err := g.WithReadLock(func(g *dag.DirectedAcyclicGraph[K]) error {
				for child := range g.Vertices[node].Edges {
					inDegree[child]--
					if inDegree[child] == 0 {
						ready = append(ready, child)
					}
				}
				return nil
			}); err != nil {
				return errors.Join(err, eg.Wait())
			}
		case <-gctx.Done():
			if err := eg.Wait(); err != nil {
				return err
			}
			// Parent context cancelled without a worker error.
			return ctx.Err()
		}
	}

	return eg.Wait()
}
