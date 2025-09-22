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

package sync

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

var (
	ErrSelfReference = fmt.Errorf("self-references are not allowed")
	ErrAlreadyExists = fmt.Errorf("vertex already exists in the graph")
)

// DirectedAcyclicGraph represents a directed acyclic graph.
// It uses a sync.Map for concurrent access. Still, it generally CANNOT be
// assumed to be thread-safe for operations that modify the graph structure.
type DirectedAcyclicGraph[T cmp.Ordered] struct {
	// Vertices stores the nodes in the graph
	Vertices *sync.Map // map[T]*Vertex[T]
}

// NewDirectedAcyclicGraph creates a new directed acyclic graph.
func NewDirectedAcyclicGraph[T cmp.Ordered]() *DirectedAcyclicGraph[T] {
	return &DirectedAcyclicGraph[T]{
		Vertices: &sync.Map{},
	}
}

// GetOutDegree returns the out-degree (number of outgoing edges) of the given
// vertex and a boolean indicating if the vertex exists in the graph.
func (d *DirectedAcyclicGraph[T]) GetOutDegree(id T) (*atomic.Int64, bool) {
	v, ok := d.GetVertex(id)
	if !ok {
		return nil, false
	}
	return &v.OutDegree, true
}

// MustGetOutDegree returns the out-degree of the given vertex, panicking if
// the vertex does not exist in the graph.
func (d *DirectedAcyclicGraph[T]) MustGetOutDegree(id T) *atomic.Int64 {
	inDegree, ok := d.GetOutDegree(id)
	if !ok {
		panic(fmt.Sprintf("out-degree for vertex %v not found in the graph", id))
	}
	return inDegree
}

// GetInDegree returns the in-degree (number of incoming edges) of the given
// vertex and a boolean indicating if the vertex exists in the graph.
func (d *DirectedAcyclicGraph[T]) GetInDegree(id T) (*atomic.Int64, bool) {
	v, ok := d.GetVertex(id)
	if !ok {
		return nil, false
	}
	return &v.InDegree, true
}

// MustGetInDegree returns the in-degree of the given vertex, panicking if
// the vertex does not exist in the graph.
func (d *DirectedAcyclicGraph[T]) MustGetInDegree(id T) *atomic.Int64 {
	inDegree, ok := d.GetInDegree(id)
	if !ok {
		panic(fmt.Sprintf("in-degree for vertex %v not found in the graph", id))
	}
	return inDegree
}

// LengthVertices returns the number of vertices in the graph.
func (d *DirectedAcyclicGraph[T]) LengthVertices() int {
	count := 0
	d.Vertices.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// Clone creates a copy of the graph and returns it.
// The copy IS NOT a complete deep copy, as it only clones the vertices and their
// and their edges. The attribute values are not cloned.
func (d *DirectedAcyclicGraph[T]) Clone() *DirectedAcyclicGraph[T] {
	cloned := NewDirectedAcyclicGraph[T]()

	d.Vertices.Range(func(key, value any) bool {
		cloned.Vertices.Store(key, value.(*Vertex[T]).Clone())
		return true
	})
	return cloned
}

// AddVertex adds a new node to the graph.
func (d *DirectedAcyclicGraph[T]) AddVertex(id T, attributes ...map[string]any) error {
	vertex := &Vertex[T]{
		ID:         id,
		Attributes: &sync.Map{},
		Edges:      &sync.Map{},
	}
	for _, attrs := range attributes {
		for k, v := range attrs {
			vertex.Attributes.Store(k, v)
		}
	}
	if actual, exists := d.Vertices.LoadOrStore(id, vertex); exists && actual != vertex {
		return fmt.Errorf("node %v already exists: %w", id, ErrAlreadyExists)
	}
	return nil
}

// DeleteVertex removes a node from the graph.
func (d *DirectedAcyclicGraph[T]) DeleteVertex(id T) error {
	if _, exists := d.Vertices.Load(id); !exists {
		return fmt.Errorf("node %v does not exist", id)
	}

	// Remove all edges to this node
	d.Vertices.Range(func(_, nodeValue any) bool {
		node := nodeValue.(*Vertex[T])
		node.Edges.Range(func(edgeKey, _ any) bool {
			if edgeKey == id {
				// Decrement the in-degree of the node
				node.InDegree.Add(-1)
				// Remove the edge from the node
				node.Edges.Delete(id)
			}
			return true
		})
		return true
	})

	d.Vertices.Delete(id)

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
	fromNode, fromExists := d.GetVertex(from)
	toNode, toExists := d.GetVertex(to)
	if !fromExists {
		return fmt.Errorf("node %v does not exist", from)
	}
	if !toExists {
		return fmt.Errorf("node %v does not exist", to)
	}
	if from == to {
		return ErrSelfReference
	}

	_, exists := fromNode.Edges.Load(to)

	if !exists {
		// Only initialize the map if the edge was added
		fromNode.Edges.Store(to, &sync.Map{})
		// Only increment the out-degree and in-degree if the edge was added
		fromNode.OutDegree.Add(1)
		toNode.InDegree.Add(1)

		// Check if the graph is still a DAG
		hasCycle, cycle := d.HasCycle()
		if hasCycle {
			// Ehmmm, we have a cycle, let's remove the edge we just added
			fromNode.Edges.Delete(to)
			toNode.InDegree.Add(-1)
			fromNode.OutDegree.Add(-1)
			return fmt.Errorf("adding an edge from %v to %v would create a cycle: %w", fmt.Sprintf("%v", from), fmt.Sprintf("%v", to), &CycleError{
				Cycle: cycle,
			})
		}
	}
	edgeVal, _ := fromNode.Edges.Load(to)

	if attrMap, ok := edgeVal.(*sync.Map); ok {
		for _, attrs := range attributes {
			for k, v := range attrs {
				attrMap.Store(k, v)
			}
		}
	}

	return nil
}

// Roots returns the root nodes of the graph, which are nodes with no incoming edges.
func (d *DirectedAcyclicGraph[T]) Roots() []T {
	var roots []T
	d.Vertices.Range(func(key, value any) bool {
		if value.(*Vertex[T]).InDegree.Load() == 0 {
			roots = append(roots, key.(T))
		}
		return true
	})
	return roots
}

// TopologicalSort performs a topological sort on the graph.
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
		var neighbors []T
		vertex, ok := d.GetVertex(node)
		if ok {
			vertex.Edges.Range(func(key, _ any) bool {
				neighbors = append(neighbors, key.(T))
				return true
			})
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

// GetVertex returns the vertex with the given ID and a boolean indicating if
// it exists in the graph.
func (d *DirectedAcyclicGraph[T]) GetVertex(id T) (*Vertex[T], bool) {
	v, ok := d.Vertices.Load(id)
	if !ok {
		return nil, false
	}
	vertex, ok := v.(*Vertex[T])
	return vertex, ok
}

// MustGetVertex returns the vertex with the given ID, panicking if the vertex
// does not exist in the graph.
func (d *DirectedAcyclicGraph[T]) MustGetVertex(id T) *Vertex[T] {
	vertex, ok := d.GetVertex(id)
	if !ok {
		panic(fmt.Sprintf("vertex %v not found in the graph", id))
	}
	return vertex
}

// GetVertices returns the nodes in the graph in sorted alphabetical order.
func (d *DirectedAcyclicGraph[T]) GetVertices() []T {
	nodes := make([]T, 0)
	d.Vertices.Range(func(key, _ any) bool {
		nodes = append(nodes, key.(T))
		return true
	})

	// Ensure deterministic order. This is important for TopologicalSort
	// to return a deterministic result.
	slices.Sort(nodes)
	return nodes
}

// GetEdges returns the edges in the graph in sorted order.
func (d *DirectedAcyclicGraph[T]) GetEdges() [][2]T {
	var edges [][2]T
	d.Vertices.Range(func(from, value any) bool {
		node := value.(*Vertex[T])
		node.Edges.Range(func(to, _ any) bool {
			edges = append(edges, [2]T{from.(T), to.(T)})
			return true
		})
		return true
	})
	sort.Slice(edges, func(i, j int) bool {
		// Sort by from node first
		if edges[i][0] == edges[j][0] {
			return edges[i][1] < edges[j][1]
		}
		return edges[i][0] < edges[j][0]
	})
	return edges
}

// HasCycle checks if the graph has a cycle. In other words, it checks whether
// the graph is still a directed ACYCLIC graph (DAG).
func (d *DirectedAcyclicGraph[T]) HasCycle() (bool, []string) {
	visited := make(map[T]bool)
	recStack := make(map[T]bool)
	var cyclePath []string

	var dfs func(T) bool
	dfs = func(node T) bool {
		visited[node] = true
		recStack[node] = true
		cyclePath = append(cyclePath, fmt.Sprintf("%v", node))

		vertex := d.MustGetVertex(node)
		foundCycle := false
		vertex.Edges.Range(func(neighbor any, _ any) bool {
			if !visited[neighbor.(T)] {
				if dfs(neighbor.(T)) {
					foundCycle = true
					return false // Stop further iteration
				}
			} else if recStack[neighbor.(T)] {
				// Found a cycle, add the closing node to complete the cycle
				cyclePath = append(cyclePath, fmt.Sprintf("%v", neighbor))
				foundCycle = true
				return false // Stop further iteration
			}
			return true
		})
		if foundCycle {
			return true
		}

		recStack[node] = false
		cyclePath = cyclePath[:len(cyclePath)-1]
		return false
	}

	var allNodes []T
	d.Vertices.Range(func(key, _ any) bool {
		allNodes = append(allNodes, key.(T))
		return true
	})

	for _, node := range allNodes {
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

// Contains checks if the graph contains a vertex with the given ID.
func (d *DirectedAcyclicGraph[T]) Contains(v T) (ok bool) {
	_, ok = d.Vertices.Load(v)
	return
}

// Reverse converts Parent → Child to Child → Parent.
// This is useful for traversing the graph in reverse order.
func (d *DirectedAcyclicGraph[T]) Reverse() (*DirectedAcyclicGraph[T], error) {
	reverse := NewDirectedAcyclicGraph[T]()

	// Ensure all vertices exist in the new graph
	// We cannot use vertex.Clone here. vertex.Clone also copies the edges.
	// But for reversing the graph, the edges have to be inverted.
	d.Vertices.Range(func(key, value any) bool {
		origVertex := value.(*Vertex[T])
		attrs := make(map[string]any)
		origVertex.Attributes.Range(func(attrKey, attrValue any) bool {
			attrs[attrKey.(string)] = attrValue
			return true
		})
		if err := reverse.AddVertex(key.(T), attrs); err != nil {
			return false
		}
		return true
	})

	d.Vertices.Range(func(key, value any) bool {
		parent := value.(*Vertex[T])
		parent.Edges.Range(func(child any, edgeAttrs any) bool {
			// Copy edge attributes
			attrMap := make(map[string]any)
			if edgeAttrs != nil {
				if smap, ok := edgeAttrs.(*sync.Map); ok {
					smap.Range(func(k, v any) bool {
						attrMap[k.(string)] = v
						return true
					})
				}
			}
			if err := reverse.AddEdge(child.(T), parent.ID, attrMap); err != nil {
				return false
			}
			return true
		})
		return true
	})

	return reverse, nil
}

// Vertex represents a node/vertex in a directed acyclic graph.
type Vertex[T cmp.Ordered] struct {
	// ID is a unique identifier for the node
	ID T
	// Attributes stores the attributes of the node, such as the component
	// descriptor.
	Attributes *sync.Map // map[string]any (attributes)
	// Edges stores the IDs of the nodes that this node has an outgoing edge to,
	// as well as any attributes associated with that edge.
	Edges *sync.Map // map[T]*sync.Map with map[string]any (attributes)

	InDegree, OutDegree atomic.Int64
}

func (v *Vertex[T]) Clone() *Vertex[T] {
	cloned := &Vertex[T]{
		ID:         v.ID,
		Attributes: &sync.Map{},
		Edges:      &sync.Map{},
	}
	cloned.InDegree.Store(v.InDegree.Load())
	cloned.OutDegree.Store(v.OutDegree.Load())
	v.Attributes.Range(func(key, value any) bool {
		k := key.(string)
		cloned.Attributes.Store(k, value)
		return true
	})
	v.Edges.Range(func(key, value any) bool {
		k := key.(T)
		if attrMap, ok := value.(*sync.Map); ok {
			newMap := &sync.Map{}
			attrMap.Range(func(kk, vv any) bool {
				newMap.Store(kk, vv)
				return true
			})
			cloned.Edges.Store(k, newMap)
		}
		return true
	})
	return cloned
}

// LengthEdges returns the number of edges (outgoing connections) from this vertex.
func (v *Vertex[T]) LengthEdges() int {
	count := 0
	v.Edges.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// EdgeKeys returns the keys of the edges of this vertex. In other words, the
// IDs of the child nodes of this vertex.
func (v *Vertex[T]) EdgeKeys() []T {
	edges := make([]T, 0)
	v.Edges.Range(func(key, _ any) bool {
		edges = append(edges, key.(T))
		return true
	})
	return edges
}

// GetAttribute retrieves an attribute from the vertex by its key and a boolean
// indicating if the attribute exists.
func (v *Vertex[T]) GetAttribute(key string) (any, bool) {
	value, ok := v.Attributes.Load(key)
	if !ok {
		return nil, false
	}
	return value, true
}

// MustGetAttribute retrieves an attribute from the vertex by its key, panicking
// if the attribute does not exist.
func (v *Vertex[T]) MustGetAttribute(key string) any {
	value, ok := v.GetAttribute(key)
	if !ok {
		panic(fmt.Sprintf("attribute %s not found in vertex %v", key, v.ID))
	}
	return value
}

// GetEdgeAttribute returns an attribute from the edge with ID to, and the
// specified attribute key. It also returns a boolean indicating if the
// attribute exists.
func (v *Vertex[T]) GetEdgeAttribute(to T, key string) (any, bool) {
	edge, ok := v.Edges.Load(to)
	if !ok {
		return nil, false
	}
	attrMap, ok := edge.(*sync.Map)
	if !ok {
		return nil, false
	}
	value, ok := attrMap.Load(key)
	if !ok {
		return nil, false
	}
	return value, true
}

// MustGetEdgeAttribute retrieves an attribute from the edge with ID to, and the
// specified attribute key, panicking if the attribute does not exist.
func (v *Vertex[T]) MustGetEdgeAttribute(to T, key string) any {
	value, ok := v.GetEdgeAttribute(to, key)
	if !ok {
		panic(fmt.Sprintf("edge attribute %s not found for edge %v -> %v", key, v.ID, to))
	}
	return value
}
