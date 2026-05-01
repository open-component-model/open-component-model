// Package dag provides Tarjan's algorithm for cycle detection in directed graphs.
//
// Paper: "Depth-First Search and Linear Graph Algorithms" (Tarjan, 1972)
// DOI: https://dl.acm.org/doi/10.1145/362342.362367

package dag

import (
	"cmp"
	"fmt"
	"maps"
	"sync"
)

// Tarjan implements Tarjan's algorithm for cycle detection.
type Tarjan struct {
	mu         sync.Mutex
	index      int
	indexMap   map[string]int
	lowlinkMap map[string]int
	onStack    map[string]bool
	stack      []string
	cycles     [][]string
}

// NewTarjan creates a new Tarjan cycle detector.
func NewTarjan() *Tarjan {
	return &Tarjan{
		indexMap:   make(map[string]int),
		lowlinkMap: make(map[string]int),
		onStack:    make(map[string]bool),
	}
}

// DetectCycles detects cycles in the given DAG and returns them.
func (t *Tarjan) DetectCycles(g *DirectedAcyclicGraph[string]) ([][]string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.index = 0
	t.indexMap = make(map[string]int)
	t.lowlinkMap = make(map[string]int)
	t.onStack = make(map[string]bool)
	t.stack = nil
	t.cycles = nil

	for vertex := range g.Vertices {
		if _, exists := t.indexMap[vertex]; !exists {
			t.strongConnect(g, vertex)
		}
	}

	if len(t.cycles) > 0 {
		return t.cycles, fmt.Errorf("cycles detected: %d", len(t.cycles))
	}
	return nil, nil
}

// strongConnect performs the DFS traversal for Tarjan's algorithm.
func (t *Tarjan) strongConnect(g *DirectedAcyclicGraph[string], vertex string) {
	t.indexMap[vertex] = t.index
	t.lowlinkMap[vertex] = t.index
	t.index++
	t.onStack[vertex] = true
	t.stack = append(t.stack, vertex)

	for neighbor := range g.Vertices[vertex].Edges {
		if _, exists := t.indexMap[neighbor]; !exists {
			t.strongConnect(g, neighbor)
			t.lowlinkMap[vertex] = min(t.lowlinkMap[vertex], t.lowlinkMap[neighbor])
		} else if t.onStack[neighbor] {
			t.lowlinkMap[vertex] = min(t.lowlinkMap[vertex], t.indexMap[neighbor])
		}
	}

	if t.lowlinkMap[vertex] == t.indexMap[vertex] {
		cycle := t.popStack(vertex)
		if len(cycle) > 1 {
			t.cycles = append(t.cycles, cycle)
		}
	}
}

// popStack pops the stack up to and including the given vertex, returning the cycle.
func (t *Tarjan) popStack(vertex string) []string {
	var cycle []string
	for {
		top := t.stack[len(t.stack)-1]
		t.stack = t.stack[:len(t.stack)-1]
		t.onStack[top] = false
		cycle = append(cycle, top)
		if top == vertex {
			break
		}
	}
	return cycle
}

// min returns the minimum of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// DetectCycles detects cycles in a DAG using Tarjan's algorithm.
func DetectCycles[T cmp.Ordered](g *DirectedAcyclicGraph[T]) ([][]T, error) {
	// Convert the graph to a string-based graph for Tarjan's algorithm.
	stringGraph := NewDirectedAcyclicGraph[string]()
	for k, v := range g.Vertices {
		stringGraph.Vertices[string(fmt.Sprintf("%v", k))] = &Vertex[string]{
			ID:       string(fmt.Sprintf("%v", k)),
			Edges:    make(map[string]map[string]any),
			InDegree: v.InDegree,
			OutDegree: v.OutDegree,
			Attributes: v.Attributes,
		}
		for neighbor, attrs := range v.Edges {
			stringGraph.Vertices[string(fmt.Sprintf("%v", k))].Edges[string(fmt.Sprintf("%v", neighbor))] = maps.Clone(attrs)
		}
	}

	// Run Tarjan's algorithm.
	tarjan := NewTarjan()
	cycles, err := tarjan.DetectCycles(stringGraph)
	if err != nil {
		// Convert cycles back to the original type.
		convertedCycles := make([][]T, len(cycles))
		for i, cycle := range cycles {
			convertedCycle := make([]T, len(cycle))
			for j, node := range cycle {
				var result T
				_, err := fmt.Sscanf(node, "%v", &result)
				if err != nil {
					// Fallback for complex types
					convertedCycle[j] = result
				} else {
					convertedCycle[j] = result
				}
			}
			convertedCycles[i] = convertedCycle
		}
		return convertedCycles, err
	}
	return nil, nil
}