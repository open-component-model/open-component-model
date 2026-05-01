package dag_test

import (
	"testing"

	"ocm.software/open-component-model/bindings/go/dag"
)

// addEdgeUnsafe adds an edge without cycle checking - for testing only
func addEdgeUnsafe(g *dag.DirectedAcyclicGraph[string], from, to string) {
	fromNode, fromExists := g.Vertices[from]
	toNode, toExists := g.Vertices[to]
	if !fromExists || !toExists {
		return
	}
	fromNode.Edges[to] = map[string]any{}
	fromNode.OutDegree++
	toNode.InDegree++
}

func TestTarjan_CycleDetection(t *testing.T) {
	tests := []struct {
		name      string
		graph     *dag.DirectedAcyclicGraph[string]
		wantCycles int
	} {
		{
			name: "No cycles",
			graph: func() *dag.DirectedAcyclicGraph[string] {
				g := dag.NewDirectedAcyclicGraph[string]()
				g.AddVertex("A")
				g.AddVertex("B")
				g.AddEdge("A", "B")
				return g
			}(),
			wantCycles: 0,
		},
		{
			name: "Single cycle",
			graph: func() *dag.DirectedAcyclicGraph[string] {
				g := dag.NewDirectedAcyclicGraph[string]()
				g.AddVertex("A")
				g.AddVertex("B")
				g.AddEdge("A", "B")
				addEdgeUnsafe(g, "B", "A")
				return g
			}(),
			wantCycles: 1,
		},
		{
			name: "Multiple cycles",
			graph: func() *dag.DirectedAcyclicGraph[string] {
				g := dag.NewDirectedAcyclicGraph[string]()
				g.AddVertex("A")
				g.AddVertex("B")
				g.AddVertex("C")
				g.AddEdge("A", "B")
				g.AddEdge("B", "C")
				addEdgeUnsafe(g, "C", "A")
				// This creates one large cycle: A->B->C->A, not two separate cycles
				return g
			}(),
			wantCycles: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cycles, err := dag.DetectCycles(tt.graph)
			if tt.wantCycles == 0 && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if tt.wantCycles > 0 && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if len(cycles) != tt.wantCycles {
				t.Fatalf("expected %d cycles, got %d", tt.wantCycles, len(cycles))
			}
		})
	}
}