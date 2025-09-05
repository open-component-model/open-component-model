package sync

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProcessTopology(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	t.Run("processes vertices in topological order", func(t *testing.T) {
		//    A
		//   / \
		//  B   C
		//   \ /
		//    D
		graph := NewDirectedAcyclicGraph[string]()
		r.NoError(graph.AddVertex("A"))
		r.NoError(graph.AddVertex("B"))
		r.NoError(graph.AddVertex("C"))
		r.NoError(graph.AddVertex("D"))
		r.NoError(graph.AddEdge("A", "B", nil))
		r.NoError(graph.AddEdge("A", "C", nil))
		r.NoError(graph.AddEdge("B", "D", nil))
		r.NoError(graph.AddEdge("C", "D", nil))

		var order []string
		processor := VertexProcessorFunc[string](func(ctx context.Context, v string) error {
			order = append(order, v)
			return nil
		})

		r.NoError(graph.ProcessTopology(ctx, processor))
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
		graph := NewDirectedAcyclicGraph[string]()
		r.NoError(graph.AddVertex("A"))
		r.NoError(graph.AddVertex("B"))
		r.NoError(graph.AddEdge("A", "B", nil))

		processor := VertexProcessorFunc[string](func(ctx context.Context, v string) error {
			if v == "B" {
				return fmt.Errorf("fail on B")
			}
			return nil
		})
		err := graph.ProcessTopology(ctx, processor)
		r.ErrorContains(err, "fail on B")
	})
}

func TestProcessReverseTopology(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	t.Run("processes vertices in reverse topological order", func(t *testing.T) {
		//    A
		//   / \
		//  B   C
		//   \ /
		//    D
		graph := NewDirectedAcyclicGraph[string]()
		r.NoError(graph.AddVertex("A"))
		r.NoError(graph.AddVertex("B"))
		r.NoError(graph.AddVertex("C"))
		r.NoError(graph.AddVertex("D"))
		r.NoError(graph.AddEdge("A", "B", nil))
		r.NoError(graph.AddEdge("A", "C", nil))
		r.NoError(graph.AddEdge("B", "D", nil))
		r.NoError(graph.AddEdge("C", "D", nil))

		var order []string
		processor := VertexProcessorFunc[string](func(ctx context.Context, v string) error {
			order = append(order, v)
			return nil
		})

		r.NoError(graph.ProcessTopology(ctx, processor, WithReverseTopology()))
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
		graph := NewDirectedAcyclicGraph[string]()
		r.NoError(graph.AddVertex("A"))
		r.NoError(graph.AddVertex("B"))
		r.NoError(graph.AddEdge("A", "B", nil))

		processor := VertexProcessorFunc[string](func(ctx context.Context, v string) error {
			if v == "A" {
				return fmt.Errorf("fail on A")
			}
			return nil
		})
		err := graph.ProcessTopology(ctx, processor, WithReverseTopology())
		r.ErrorContains(err, "fail on A")
	})
}
