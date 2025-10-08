package sync

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/dag"
)

func TestProcessTopology(t *testing.T) {
	t.Run("processes vertices in topological order", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		//    A
		//   / \
		//  B   C
		//   \ /
		//    D
		graph := dag.NewDirectedAcyclicGraph[string]()
		r.NoError(graph.AddVertex("A", map[string]any{AttributeValue: "A"}))
		r.NoError(graph.AddVertex("B", map[string]any{AttributeValue: "B"}))
		r.NoError(graph.AddVertex("C", map[string]any{AttributeValue: "C"}))
		r.NoError(graph.AddVertex("D", map[string]any{AttributeValue: "D"}))
		r.NoError(graph.AddEdge("A", "B", nil))
		r.NoError(graph.AddEdge("A", "C", nil))
		r.NoError(graph.AddEdge("B", "D", nil))
		r.NoError(graph.AddEdge("C", "D", nil))

		var orderMu sync.Mutex
		var order []string
		processorFunc := ProcessorFunc[string](func(ctx context.Context, v string) error {
			// prevent data race
			orderMu.Lock()
			defer orderMu.Unlock()
			order = append(order, v)
			return nil
		})

		processor := NewGraphProcessor(ToSyncedGraph(graph), &GraphProcessorOptions[string, string]{
			Processor: processorFunc,
		})

		r.NoError(processor.Process(ctx))
		// D must be last, A must be first, B and C after A, before D
		idxMap := make(map[string]int)
		for i, v := range order {
			idxMap[v] = i
		}
		r.True(idxMap["A"] < idxMap["B"], "A before B")
		r.True(idxMap["A"] < idxMap["C"], "A before C")
		r.True(idxMap["B"] < idxMap["D"], "B before D")
		r.True(idxMap["C"] < idxMap["D"], "C before D")
		r.Equal(4, len(order), "all vertices processed")
	})

	t.Run("returns error if processor fails", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		graph := dag.NewDirectedAcyclicGraph[string]()
		r.NoError(graph.AddVertex("A", map[string]any{AttributeValue: "A"}))
		r.NoError(graph.AddVertex("B", map[string]any{AttributeValue: "B"}))
		r.NoError(graph.AddEdge("A", "B", nil))

		processorFunc := ProcessorFunc[string](func(ctx context.Context, v string) error {
			if v == "B" {
				return fmt.Errorf("fail on B")
			}
			return nil
		})
		processor := NewGraphProcessor(ToSyncedGraph(graph), &GraphProcessorOptions[string, string]{
			Processor: processorFunc,
		})

		r.ErrorContains(processor.Process(ctx), "fail on B")
	})
}

func TestProcessReverseTopology(t *testing.T) {
	t.Run("processes vertices in reverse topological order", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		//    A
		//   / \
		//  B   C
		//   \ /
		//    D
		graph := dag.NewDirectedAcyclicGraph[string]()
		r.NoError(graph.AddVertex("A", map[string]any{AttributeValue: "A"}))
		r.NoError(graph.AddVertex("B", map[string]any{AttributeValue: "B"}))
		r.NoError(graph.AddVertex("C", map[string]any{AttributeValue: "C"}))
		r.NoError(graph.AddVertex("D", map[string]any{AttributeValue: "D"}))
		r.NoError(graph.AddEdge("A", "B", nil))
		r.NoError(graph.AddEdge("A", "C", nil))
		r.NoError(graph.AddEdge("B", "D", nil))
		r.NoError(graph.AddEdge("C", "D", nil))

		var orderMu sync.Mutex
		var order []string
		processorFunc := ProcessorFunc[string](func(ctx context.Context, v string) error {
			// prevent data race
			orderMu.Lock()
			defer orderMu.Unlock()
			order = append(order, v)
			return nil
		})

		graph, err := graph.Reverse()
		r.NoError(err)

		processor := NewGraphProcessor(ToSyncedGraph(graph), &GraphProcessorOptions[string, string]{
			Processor: processorFunc,
		})

		r.NoError(processor.Process(ctx))
		// A must be last, D must be first, B and C after D, before A
		idxMap := make(map[string]int)
		for i, v := range order {
			idxMap[v] = i
		}
		r.True(idxMap["D"] < idxMap["B"], "D before B")
		r.True(idxMap["D"] < idxMap["C"], "D before C")
		r.True(idxMap["B"] < idxMap["A"], "B before A")
		r.True(idxMap["C"] < idxMap["A"], "C before A")
		r.Equal(4, len(order), "all vertices processed")
	})

	t.Run("returns error if processor fails", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		graph := dag.NewDirectedAcyclicGraph[string]()
		r.NoError(graph.AddVertex("A", map[string]any{AttributeValue: "A"}))
		r.NoError(graph.AddVertex("B", map[string]any{AttributeValue: "A"}))
		r.NoError(graph.AddEdge("A", "B", nil))

		processorFunc := ProcessorFunc[string](func(ctx context.Context, v string) error {
			if v == "A" {
				return fmt.Errorf("fail on A")
			}
			return nil
		})

		processor := NewGraphProcessor(ToSyncedGraph(graph), &GraphProcessorOptions[string, string]{
			Processor: processorFunc,
		})
		err := processor.Process(ctx)
		r.ErrorContains(err, "fail on A")
	})
}

func TestProcessTopology_ConcurrentExecution(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	//    A
	//   / \
	//  B   C
	//   \ /
	//    D
	graph := dag.NewDirectedAcyclicGraph[string]()
	r.NoError(graph.AddVertex("A", map[string]any{AttributeValue: "A"}))
	r.NoError(graph.AddVertex("B", map[string]any{AttributeValue: "B"}))
	r.NoError(graph.AddVertex("C", map[string]any{AttributeValue: "C"}))
	r.NoError(graph.AddVertex("D", map[string]any{AttributeValue: "D"}))
	r.NoError(graph.AddEdge("A", "B", nil))
	r.NoError(graph.AddEdge("A", "C", nil))
	r.NoError(graph.AddEdge("B", "D", nil))
	r.NoError(graph.AddEdge("C", "D", nil))

	var concurrent int32
	var maxConcurrent int32
	processorFunc := ProcessorFunc[string](func(ctx context.Context, v string) error {
		cur := atomic.AddInt32(&concurrent, 1)
		defer atomic.AddInt32(&concurrent, -1)
		atomic.StoreInt32(&maxConcurrent, max(atomic.LoadInt32(&maxConcurrent), cur))
		time.Sleep(100 * time.Millisecond)
		return nil
	})

	processor := NewGraphProcessor(ToSyncedGraph(graph), &GraphProcessorOptions[string, string]{
		Processor:   processorFunc,
		Concurrency: 2, // enforce limit
	})

	r.NoError(processor.Process(ctx))

	r.LessOrEqual(int(maxConcurrent), 2, "must never exceed concurrency limit 2")
}
