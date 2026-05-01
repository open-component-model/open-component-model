package benchmarks

import (
	"fmt"
	"testing"

	dag "ocm.software/open-component-model/bindings/go/dag"
)

func BenchmarkCycleDetection(b *testing.B) {
	tests := []struct {
		name  string
		size  int
		cycle bool
	} {
		{"1K nodes, no cycle", 1000, false},
		{"1K nodes, with cycle", 1000, true},
		{"10K nodes, no cycle", 10000, false},
		{"10K nodes, with cycle", 10000, true},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			g := generateGraph(tt.size, tt.cycle)
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, _ = dag.DetectCycles(g)
			}
		})
	}
}

// generateGraph generates a synthetic graph for benchmarking.
func generateGraph(size int, withCycle bool) *dag.DirectedAcyclicGraph[string] {
	g := dag.NewDirectedAcyclicGraph[string]()
	for i := 0; i < size; i++ {
		g.AddVertex(fmt.Sprintf("node-%d", i))
		if i > 0 {
			g.AddEdge(fmt.Sprintf("node-%d", i-1), fmt.Sprintf("node-%d", i))
		}
	}
	if withCycle {
		g.AddEdge(fmt.Sprintf("node-%d", size-1), "node-0")
	}
	return g
}