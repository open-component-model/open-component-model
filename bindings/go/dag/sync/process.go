// Package sync provides concurrency-safe utilities for processing directed
// acyclic graphs (DAGs). It supports concurrent, dependency-respecting graph
// traversal using Kahn's algorithm with configurable parallelism.
package sync

import (
	"cmp"
	"context"
	"fmt"
	"slices"

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
// Processor to each node. It implements a batched, parallel form of Kahn's
// algorithm:
//
//  1. Compute in-degrees for all vertices.
//  2. Collect roots (in-degree == 0) into a queue.
//  3. For each batch of ready nodes:
//     - Process them concurrently (bounded by Concurrency).
//     - Decrement children in-degrees.
//     - Add newly unlocked nodes to the queue.
//  4. Repeat until the graph is fully processed.
//
// Concurrency ensures that independent nodes are processed in parallel while
// respecting dependency ordering.
func (d *GraphProcessor[K, V]) Process(ctx context.Context) error {
	g := d.graph

	inDegree := make(map[K]int)
	var queue []K

	// Initialize in-degree map and root nodes.
	err := g.WithReadLock(func(g *dag.DirectedAcyclicGraph[K]) error {
		for id, v := range g.Vertices {
			inDegree[id] = v.InDegree
			if v.InDegree == 0 {
				queue = append(queue, id)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to read graph: %w", err)
	}

	for len(queue) > 0 {
		errGroup, gctx := errgroup.WithContext(ctx)
		if d.opts.Concurrency > 0 {
			errGroup.SetLimit(d.opts.Concurrency)
		}

		batch := slices.Clone(queue)
		queue = nil

		for _, node := range batch {
			errGroup.Go(func() error {
				if err := d.opts.Processor.ProcessValue(gctx, d.CurrentValue(node)); err != nil {
					return fmt.Errorf("failed to process value for node %v in graph: %w", node, err)
				}
				return nil
			})
		}

		// Wait until the batch is fully processed.
		if err := errGroup.Wait(); err != nil {
			return err
		}

		// Update in-degrees of children and enqueue unlocked nodes.
		if err := g.WithReadLock(func(g *dag.DirectedAcyclicGraph[K]) error {
			for _, node := range batch {
				for child := range g.Vertices[node].Edges {
					inDegree[child]--
					if inDegree[child] == 0 {
						queue = append(queue, child)
					}
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}

	return nil
}
