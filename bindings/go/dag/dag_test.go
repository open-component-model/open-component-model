// # Modified from https://github.com/kro-run/kro/blob/7e437f2fe159a1e1c59d8eefd2bfa55320df4489/pkg/graph/dag/dag.go under Apache 2.0 License
//
// Original License:
//
// Copyright 2025 The Kube Resource Orchestrator Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.
//
// We would like to thank the authors of kro for their outstanding work on this code.

package dag

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDAGAddNode(t *testing.T) {
	r := require.New(t)
	d := NewDirectedAcyclicGraph[string]()

	r.NoError(d.AddVertex("A", map[string]any{"key": "1"}))
	r.Error(d.AddVertex("A", map[string]any{"key": "2"}), "duplicate node ids are forbidden")

	r.True(d.Contains("A"))
	r.False(d.Contains("B"))

	r.Lenf(d.Vertices, 1, "expected 1 node after rejection of the second, but got %d", len(d.Vertices))
	r.Equal("A", d.Vertices["A"].ID, "expected node ID to be 'A', but got %s", d.Vertices["A"].ID)
	r.Equal("1", d.Vertices["A"].Attributes["key"], "expected node attribute to be '1', but got %s", d.Vertices["A"].Attributes["key"])

	r.NoError(d.AddVertex("B", map[string]any{"key": "2"}))
	r.Lenf(d.Vertices, 2, "expected 2 nodes after adding 'B', but got %d", len(d.Vertices))
	r.Equal("B", d.Vertices["B"].ID, "expected node ID to be 'B', but got %s", d.Vertices["B"].ID)
	r.Equal("2", d.Vertices["B"].Attributes["key"], "expected node attribute to be '2', but got %s", d.Vertices["B"].Attributes["key"])

	t.Run("roots", func(t *testing.T) {
		r := require.New(t)
		roots := d.Roots()
		r.Len(roots, 2, "expected 2 roots, but got %d", len(d.Roots()))
		r.ElementsMatch([]string{"A", "B"}, d.Roots(), "expected roots to be [A B], but got %v", d.Roots())
	})

	t.Run("degrees", func(t *testing.T) {
		r := require.New(t)
		r.Equal(d.Vertices["A"].OutDegree, 0, "expected out-degree of A to be 0, but got %d", d.Vertices["A"].OutDegree)
		r.Equal(d.Vertices["A"].InDegree, 0, "expected in-degree of A to be 0, but got %d", d.Vertices["A"].InDegree)
		r.Equal(d.Vertices["B"].OutDegree, 0, "expected out-degree of B to be 0, but got %d", d.Vertices["B"].OutDegree)
		r.Equal(d.Vertices["B"].InDegree, 0, "expected in-degree of B to be 0, but got %d", d.Vertices["B"].InDegree)
	})

	t.Run("delete", func(t *testing.T) {
		r := require.New(t)
		r.NoError(d.DeleteVertex("A"))
		r.Lenf(d.Vertices, 1, "expected 1 node after deleting 'A', but got %d", len(d.Vertices))
		r.Equal("B", d.Vertices["B"].ID, "expected node ID to be 'B', but got %s", d.Vertices["B"].ID)
		r.Error(d.DeleteVertex("A"), "expected error when deleting non-existent node 'A', but got nil")
		_, outExists := d.Vertices["A"]
		r.False(outExists)
		_, inExists := d.Vertices["A"]
		r.False(inExists)
	})
}

func TestDAGAddEdge(t *testing.T) {
	r := require.New(t)
	d := NewDirectedAcyclicGraph[string]()
	r.NoError(d.AddVertex("A"))
	r.NoError(d.AddVertex("B"))
	r.NoError(d.AddEdge("A", "B", map[string]any{"key": "1"}))

	t.Run("roots", func(t *testing.T) {
		r := require.New(t)
		roots := d.Roots()
		r.Len(roots, 1, "expected 1 root (A), but got %d", len(d.Roots()))
		r.EqualValues([]string{"A"}, d.Roots(), "expected roots to be [A], but got %v", d.Roots())
	})

	r.Len(d.Vertices["A"].Edges, 1, "expected 1 edge from A to B, but got %d", len(d.Vertices["A"].Edges))
	r.EqualValues([]string{"B"}, slices.Collect(maps.Keys(d.Vertices["A"].Edges)), "expected edge ID to be 'B', but got %s", d.Vertices["A"].Edges)
	r.Len(d.Vertices["B"].Edges, 0, "expected 0 edges from B to A, but got %d", len(d.Vertices["B"].Edges))

	t.Run("degrees", func(t *testing.T) {
		r.Equal(d.Vertices["A"].OutDegree, 1, "expected out-degree of A to be 1, but got %d", d.Vertices["A"].OutDegree)
		r.Equal(d.Vertices["A"].InDegree, 0, "expected in-degree of A to be 0, but got %d", d.Vertices["A"].InDegree)

		r.Equal(d.Vertices["B"].OutDegree, 0, "expected out-degree of B to be 0, but got %d", d.Vertices["B"].OutDegree)
		r.Equal(d.Vertices["B"].InDegree, 1, "expected in-degree of B to be 1, but got %d", d.Vertices["B"].InDegree)
	})

	t.Run("reverse", func(t *testing.T) {
		r := require.New(t)
		d, err := d.Reverse()
		r.NoError(err, "error reversing the graph")
		r.Len(d.Vertices["A"].Edges, 0, "expected 0 edges from A to B, but got %d", len(d.Vertices["A"].Edges))
		r.Len(d.Vertices["B"].Edges, 1, "expected 1 edge from B to A, but got %d", len(d.Vertices["B"].Edges))
		r.EqualValues([]string{"A"}, slices.Collect(maps.Keys(d.Vertices["B"].Edges)), "expected edge ID to be 'A', but got %s", d.Vertices["B"].Edges)
	})

	t.Run("delete", func(t *testing.T) {
		r := require.New(t)
		r.NoError(d.DeleteVertex("A"))
		r.Lenf(d.Vertices, 1, "expected 1 node after deleting 'A', but got %d", len(d.Vertices))
		r.Equal("B", d.Vertices["B"].ID, "expected node ID to be 'B', but got %s", d.Vertices["B"].ID)
		r.Error(d.DeleteVertex("A"), "expected error when deleting non-existent node 'A', but got nil")
		_, outExists := d.Vertices["A"]
		r.False(outExists)
		_, inExists := d.Vertices["A"]
		r.False(inExists)
	})
}

func TestDAGHasCycle(t *testing.T) {
	r := require.New(t)
	d := NewDirectedAcyclicGraph[string]()
	r.NoError(d.AddVertex("A"))
	r.NoError(d.AddVertex("B"))
	r.NoError(d.AddVertex("C"))

	r.NoError(d.AddEdge("A", "B"))
	r.NoError(d.AddEdge("B", "C"))

	cyclic, _ := d.HasCycle()
	r.False(cyclic, "DAG incorrectly reported a cycle")

	r.Error(d.AddEdge("C", "A"), "expected error when creating a cycle, but got nil")

	// pointless to test for the cycle here, so we need to emulate one
	// by artificially adding a cycle.
	d.Vertices["C"].Edges["A"] = map[string]any{}
	cyclic, _ = d.HasCycle()
	r.Truef(cyclic, "DAG incorrectly reported no cycle")

	_, err := d.TopologicalSort()
	r.Errorf(err, "expected error when sorting a cyclic graph, but got nil")
	r.IsType(&CycleError{}, err, "expected CycleError, but got %T", err)

	var cerr *CycleError
	r.True(errors.As(err, &cerr))
	cycle := cerr.Cycle

	r.Len(cycle, 4)

	possible := [][]string{
		{"A", "B", "C", "A"},
		{"B", "C", "A", "B"},
		{"C", "A", "B", "C"},
	}

	match := false
	for _, combination := range possible {
		if slices.Equal(combination, cycle) {
			match = true
			break
		}
	}
	r.Truef(match, "expected cyclic graph cycle, one of %v but got %v", possible, cycle)
}

func TestDAGTopologicalSort(t *testing.T) {
	grid := []struct {
		Nodes string
		Edges string
		Want  string
	}{
		{Nodes: "A,B", Want: "A,B"},
		{Nodes: "A,B", Edges: "A->B", Want: "B,A"},
		{Nodes: "A,B", Edges: "B->A", Want: "A,B"},
		{Nodes: "A,B,C,D,E,F", Edges: "", Want: "A,B,C,D,E,F"},
		{Nodes: "A,B,C,D,E,F", Edges: "C->D", Want: "A,B,C,D,E,F"},
		{Nodes: "A,B,C,D,E,F", Edges: "D->C", Want: "A,B,D,E,F,C"},
		{Nodes: "A,B,C,D,E,F", Edges: "F->A,F->B,B->A", Want: "C,D,E,F,B,A"},
		{Nodes: "A,B,C,D,E,F", Edges: "B->A,C->A,D->B,D->C,F->E,A->E", Want: "D,F,B,C,A,E"},
	}

	for i, g := range grid {
		t.Run(fmt.Sprintf("[%d] nodes=%s,edges=%s", i, g.Nodes, g.Edges), func(t *testing.T) {
			r := require.New(t)
			d := NewDirectedAcyclicGraph[string]()
			for _, node := range strings.Split(g.Nodes, ",") {
				r.NoError(d.AddVertex(node))
			}

			if g.Edges != "" {
				for _, edge := range strings.Split(g.Edges, ",") {
					tokens := strings.SplitN(edge, "->", 2)
					r.NoError(d.AddEdge(tokens[0], tokens[1]))
				}
			}

			order, err := d.TopologicalSort()
			r.NoError(err, "error sorting the graph")

			expected := strings.Split(g.Want, ",")
			r.ElementsMatch(order, expected, "unexpected result from TopologicalSort for nodes=%q edges=%q, got %q, want %q", g.Nodes, g.Edges, order, expected)

			// checkValidTopologicalOrder(t, d, order)
		})
	}
}
