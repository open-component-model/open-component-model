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
	"cmp"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
)

var ErrSelfReference = fmt.Errorf("self-references are not allowed")

// Vertex represents a node/vertex in a directed acyclic graph.
type Vertex[T cmp.Ordered] struct {
	// ID is a unique identifier for the node
	ID T
	// Attributes stores the attributes of the node, such as the component
	// descriptor.
	Attributes map[string]any
	// Edges stores the IDs of the nodes that this node has an outgoing edge to.
	// In kro, this would be the children of a resource.
	Edges map[T]map[string]any

	InDegree, OutDegree int
}

// DirectedAcyclicGraph represents a directed acyclic graph.
type DirectedAcyclicGraph[T cmp.Ordered] struct {
	// Vertices stores the nodes in the graph
	Vertices map[T]*Vertex[T]
}

// NewDirectedAcyclicGraph creates a new directed acyclic graph.
func NewDirectedAcyclicGraph[T cmp.Ordered]() *DirectedAcyclicGraph[T] {
	return &DirectedAcyclicGraph[T]{
		Vertices: make(map[T]*Vertex[T]),
	}
}

func (d *DirectedAcyclicGraph[T]) Clone() *DirectedAcyclicGraph[T] {
	return &DirectedAcyclicGraph[T]{
		Vertices: maps.Clone(d.Vertices),
	}
}

// AddVertex adds a new node to the graph.
func (d *DirectedAcyclicGraph[T]) AddVertex(id T, attributes ...map[string]any) error {
	if _, exists := d.Vertices[id]; exists {
		return fmt.Errorf("node %v already exists", id)
	}
	d.Vertices[id] = &Vertex[T]{
		ID:         id,
		Attributes: make(map[string]any),
		Edges:      make(map[T]map[string]any),
		InDegree:   0,
		OutDegree:  0,
	}

	for _, attributes := range attributes {
		maps.Copy(d.Vertices[id].Attributes, attributes)
	}
	return nil
}

// DeleteVertex removes a node from the graph.
func (d *DirectedAcyclicGraph[T]) DeleteVertex(id T) error {
	if _, exists := d.Vertices[id]; !exists {
		return fmt.Errorf("node %v does not exist", id)
	}

	// Remove all edges to this node
	for _, node := range d.Vertices {
		if _, exists := node.Edges[id]; exists {
			// Decrement the in-degree of the node
			d.Vertices[node.ID].InDegree--
			// Remove the edge from the node
			delete(node.Edges, id)
		}
	}

	delete(d.Vertices, id)
	return nil
}

type CycleError struct {
	Cycle []string
}

func (e *CycleError) Error() string {
	return fmt.Sprintf("The current graph would create a cycle: %s", formatCycle(e.Cycle))
}

func formatCycle(cycle []string) string {
	return strings.Join(cycle, " -> ")
}

// AddEdge adds a directed edge from one node to another.
func (d *DirectedAcyclicGraph[T]) AddEdge(from, to T, attributes ...map[string]any) error {
	fromNode, fromExists := d.Vertices[from]
	toNode, toExists := d.Vertices[to]
	if !fromExists {
		return fmt.Errorf("node %v does not exist", from)
	}
	if !toExists {
		return fmt.Errorf("node %v does not exist", to)
	}
	if from == to {
		return ErrSelfReference
	}

	_, exists := fromNode.Edges[to]

	if !exists {
		// Only initialize the map if the edge was added
		fromNode.Edges[to] = map[string]any{}
		// Only increment the out-degree and in-degree if the edge was added
		fromNode.OutDegree++
		toNode.InDegree++

		// Check if the graph is still a DAG
		hasCycle, cycle := d.HasCycle()
		if hasCycle {
			// Ehmmm, we have a cycle, let's remove the edge we just added
			delete(fromNode.Edges, to)
			fromNode.OutDegree--
			toNode.InDegree--

			return fmt.Errorf("adding an edge from %v to %v would create a cycle: %w", fmt.Sprintf("%v", from), fmt.Sprintf("%v", to), &CycleError{
				Cycle: cycle,
			})
		}
	}

	for _, attributes := range attributes {
		maps.Copy(fromNode.Edges[to], attributes)
	}

	return nil
}

func (d *DirectedAcyclicGraph[T]) Roots() []T {
	var roots []T
	for key, node := range d.Vertices {
		if node.InDegree == 0 {
			roots = append(roots, key)
		}
	}
	return roots
}

func (d *DirectedAcyclicGraph[T]) TopologicalSort() ([]T, error) {
	if cyclic, nodes := d.HasCycle(); cyclic {
		return nil, &CycleError{
			Cycle: nodes,
		}
	}

	visited := make(map[T]bool)
	var order []T

	// Get a sorted list of all vertices
	vertices := d.GetVertices()

	var dfs func(T)
	dfs = func(node T) {
		visited[node] = true

		// Sort the neighbors to ensure deterministic order
		neighbors := make([]T, 0, len(d.Vertices[node].Edges))
		for neighbor := range d.Vertices[node].Edges {
			neighbors = append(neighbors, neighbor)
		}
		slices.Sort(neighbors)

		for _, neighbor := range neighbors {
			if !visited[neighbor] {
				dfs(neighbor)
			}
		}
		order = append(order, node)
	}

	// Visit nodes in a deterministic order
	for _, node := range vertices {
		if !visited[node] {
			dfs(node)
		}
	}

	return order, nil
}

// GetVertices returns the nodes in the graph in sorted alphabetical
// order.
func (d *DirectedAcyclicGraph[T]) GetVertices() []T {
	nodes := make([]T, 0, len(d.Vertices))
	for node := range d.Vertices {
		nodes = append(nodes, node)
	}

	// Ensure deterministic order. This is important for TopologicalSort
	// to return a deterministic result.
	slices.Sort(nodes)
	return nodes
}

// GetEdges returns the edges in the graph in sorted order...
func (d *DirectedAcyclicGraph[T]) GetEdges() [][2]T {
	var edges [][2]T
	for from, node := range d.Vertices {
		for to := range node.Edges {
			edges = append(edges, [2]T{from, to})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		// Sort by from node first
		if edges[i][0] == edges[j][0] {
			return edges[i][1] < edges[j][1]
		}
		return edges[i][0] < edges[j][0]
	})
	return edges
}

func (d *DirectedAcyclicGraph[T]) HasCycle() (bool, []string) {
	visited := make(map[T]bool)
	recStack := make(map[T]bool)
	var cyclePath []string

	var dfs func(T) bool
	dfs = func(node T) bool {
		visited[node] = true
		recStack[node] = true
		cyclePath = append(cyclePath, fmt.Sprintf("%v", node))

		for neighbor := range d.Vertices[node].Edges {
			if !visited[neighbor] {
				if dfs(neighbor) {
					return true
				}
			} else if recStack[neighbor] {
				// Found a cycle, add the closing node to complete the cycle
				cyclePath = append(cyclePath, fmt.Sprintf("%v", neighbor))
				return true
			}
		}

		recStack[node] = false
		cyclePath = cyclePath[:len(cyclePath)-1]
		return false
	}

	for node := range d.Vertices {
		if !visited[node] {
			cyclePath = []string{}
			if dfs(node) {
				// Trim the cycle path to start from the repeated node
				start := 0
				for i, v := range cyclePath[:len(cyclePath)-1] {
					if v == cyclePath[len(cyclePath)-1] {
						start = i
						break
					}
				}
				return true, cyclePath[start:]
			}
		}
	}

	return false, nil
}

func (d *DirectedAcyclicGraph[T]) Contains(v T) (ok bool) {
	_, ok = d.Vertices[v]
	return
}

// Reverse converts Parent → Child to Child → Parent.
// This is useful for traversing the graph in reverse order.
func (d *DirectedAcyclicGraph[T]) Reverse() (*DirectedAcyclicGraph[T], error) {
	reverse := NewDirectedAcyclicGraph[T]()

	// Ensure all vertices exist in the new graph
	for _, parent := range d.Vertices {
		if err := reverse.AddVertex(parent.ID); err != nil {
			return nil, err
		}
	}

	// Reverse the edges: Child -> Parent instead of Parent -> Child
	for _, parent := range d.Vertices {
		for child := range parent.Edges {
			if err := reverse.AddEdge(child, parent.ID); err != nil {
				return nil, err
			}
		}
	}

	return reverse, nil
}
