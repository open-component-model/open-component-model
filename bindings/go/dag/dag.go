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
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
)

var (
	ErrSelfReference = fmt.Errorf("self-references are not allowed")
	ErrAlreadyExists = fmt.Errorf("vertex already exists in the graph")
)

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
}

func (v *Vertex[T]) Clone() *Vertex[T] {
	cloned := &Vertex[T]{
		ID:         v.ID,
		Attributes: &sync.Map{},
		Edges:      &sync.Map{},
	}
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

// DirectedAcyclicGraph represents a directed acyclic graph.
type DirectedAcyclicGraph[T cmp.Ordered] struct {
	// Vertices stores the nodes in the graph
	Vertices *sync.Map // map[T]*Vertex[T]
	// OutDegree of each vertex (number of outgoing edges)
	OutDegree *sync.Map // map[T]int
	// InDegree of each vertex (number of incoming edges)
	InDegree *sync.Map // map[T]int
}

// NewDirectedAcyclicGraph creates a new directed acyclic graph.
func NewDirectedAcyclicGraph[T cmp.Ordered]() *DirectedAcyclicGraph[T] {
	return &DirectedAcyclicGraph[T]{
		Vertices:  &sync.Map{},
		OutDegree: &sync.Map{},
		InDegree:  &sync.Map{},
	}
}

func (d *DirectedAcyclicGraph[T]) GetOutDegree(id T) (int, bool) {
	if outDegree, ok := d.OutDegree.Load(id); ok {
		return outDegree.(int), true
	}
	return 0, false
}

func (d *DirectedAcyclicGraph[T]) GetInDegree(id T) (int, bool) {
	if inDegree, ok := d.InDegree.Load(id); ok {
		return inDegree.(int), true
	}
	return 0, false
}

func (d *DirectedAcyclicGraph[T]) Clone() *DirectedAcyclicGraph[T] {
	cloned := NewDirectedAcyclicGraph[T]()
	d.Vertices.Range(func(key, value any) bool {
		cloned.Vertices.Store(key, value.(*Vertex[T]).Clone())
		return true
	})
	d.OutDegree.Range(func(key, value any) bool {
		cloned.OutDegree.Store(key, value)
		return true
	})
	d.InDegree.Range(func(key, value any) bool {
		cloned.InDegree.Store(key, value)
		return true
	})
	return cloned
}

// AddVertex adds a new node to the graph.
func (d *DirectedAcyclicGraph[T]) AddVertex(id T, attributes ...map[string]any) error {
	if _, exists := d.Vertices.Load(id); exists {
		return fmt.Errorf("node %v already exists: %w", id, ErrAlreadyExists)
	}
	vertex := &Vertex[T]{
		ID:         id,
		Attributes: &sync.Map{},
		Edges:      &sync.Map{},
	}
	d.Vertices.Store(id, vertex)

	for _, attrs := range attributes {
		for k, v := range attrs {
			vertex.Attributes.Store(k, v)
		}
	}

	d.OutDegree.Store(id, 0)
	d.InDegree.Store(id, 0)
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
				inDegree, _ := d.InDegree.Load(node.ID)
				d.InDegree.Store(node.ID, inDegree.(int)-1)
				// Remove the edge from the node
				node.Edges.Delete(id)
			}
			return true
		})
		return true
	})

	d.Vertices.Delete(id)
	d.OutDegree.Delete(id)
	d.InDegree.Delete(id)

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
	_, toExists := d.GetVertex(to)
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
		attrMap := &sync.Map{}
		fromNode.Edges.Store(to, attrMap)
		// Only increment the out-degree and in-degree if the edge was added
		outDegree, _ := d.OutDegree.Load(from)
		d.OutDegree.Store(from, outDegree.(int)+1)
		inDegree, _ := d.InDegree.Load(to)
		d.InDegree.Store(to, inDegree.(int)+1)

		// Check if the graph is still a DAG
		hasCycle, cycle := d.HasCycle()
		if hasCycle {
			// Ehmmm, we have a cycle, let's remove the edge we just added
			fromNode.Edges.Delete(to)
			d.OutDegree.Store(from, outDegree.(int)-1)
			d.InDegree.Store(to, inDegree.(int)-1)
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

func (d *DirectedAcyclicGraph[T]) Roots() []T {
	var roots []T
	d.InDegree.Range(func(key, value any) bool {
		if value.(int) == 0 {
			roots = append(roots, key.(T))
		}
		return true
	})
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

func (d *DirectedAcyclicGraph[T]) GetVertex(id T) (*Vertex[T], bool) {
	v, ok := d.Vertices.Load(id)
	if !ok {
		return nil, false
	}
	vertex, ok := v.(*Vertex[T])
	return vertex, ok
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

// GetEdges returns the edges in the graph in sorted order...
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

func (d *DirectedAcyclicGraph[T]) HasCycle() (bool, []string) {
	visited := make(map[T]bool)
	recStack := make(map[T]bool)
	var cyclePath []string

	var dfs func(T) bool
	dfs = func(node T) bool {
		visited[node] = true
		recStack[node] = true
		cyclePath = append(cyclePath, fmt.Sprintf("%v", node))

		vertex, ok := d.GetVertex(node)
		if ok {
			vertex.Edges.Range(func(neighbor any, _ any) bool {
				if !visited[neighbor.(T)] {
					if dfs(neighbor.(T)) {
						return true
					}
				} else if recStack[neighbor.(T)] {
					// Found a cycle, add the closing node to complete the cycle
					cyclePath = append(cyclePath, fmt.Sprintf("%v", neighbor))
					return true
				}
				return true
			})
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

func (d *DirectedAcyclicGraph[T]) Contains(v T) (ok bool) {
	_, ok = d.Vertices.Load(v)
	return
}

// Reverse converts Parent → Child to Child → Parent.
// This is useful for traversing the graph in reverse order.
func (d *DirectedAcyclicGraph[T]) Reverse() (*DirectedAcyclicGraph[T], error) {
	reverse := NewDirectedAcyclicGraph[T]()

	// Ensure all vertices exist in the new graph
	d.Vertices.Range(func(key, value any) bool {
		if err := reverse.AddVertex(key.(T)); err != nil {
			return false
		}
		return true
	})

	// Reverse the edges: Child -> Parent instead of Parent -> Child
	d.Vertices.Range(func(key, value any) bool {
		parent := value.(*Vertex[T])
		parent.Edges.Range(func(child any, _ any) bool {
			if err := reverse.AddEdge(child.(T), parent.ID); err != nil {
				return false
			}
			return true
		})
		return true
	})

	return reverse, nil
}

type DiscoveryState int

const (
	AttributeDiscoveryState = "dag/discovery-state"
	AttributeOrderIndex     = "dag/order-index"

	StateDiscovering DiscoveryState = iota
	StateDiscovered
	StateCompleted
	StateError
)

// Discover recursively discovers the graph from the rootID vertex using the provided process function.
func (d *DirectedAcyclicGraph[T]) Discover(ctx context.Context, root *Vertex[T], process func(ctx context.Context, v *Vertex[T]) (neighbors []*Vertex[T], err error)) error {
	if err := d.AddVertex(root.ID, root.AttributesToMap(), map[string]any{
		AttributeDiscoveryState: StateDiscovering,
	}); err != nil && !errors.Is(err, ErrAlreadyExists) {
		return fmt.Errorf("failed to add vertex for rootID %v: %w", root, err)
	}
	return d.discover(ctx, root.ID, process, &sync.Map{})
}

func (d *DirectedAcyclicGraph[T]) discover(ctx context.Context, id T, process func(ctx context.Context, v *Vertex[T]) (neighbors []*Vertex[T], err error), doneMap *sync.Map) error {
	doneCh, loaded := doneMap.LoadOrStore(id, make(chan struct{}))
	done := doneCh.(chan struct{})
	if loaded {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
		}
		return nil
	}
	defer close(done)

	vertex, ok := d.GetVertex(id)
	if !ok {
		return fmt.Errorf("vertex %v not found in the graph", id)
	}

	neighbors, err := process(ctx, vertex)
	if err != nil {
		vertex.Attributes.Store(AttributeDiscoveryState, StateError)
		return fmt.Errorf("failed to process id %v: %w", id, err)
	}
	vertex.Attributes.Store(AttributeDiscoveryState, StateDiscovered)

	wg, ctx := errgroup.WithContext(ctx)
	for index, ref := range neighbors {
		if err := d.AddVertex(ref.ID, ref.AttributesToMap(), map[string]any{
			AttributeDiscoveryState: StateDiscovering},
		); err != nil && !errors.Is(err, ErrAlreadyExists) {
			return fmt.Errorf("failed to add vertex for reference %v: %w", ref, err)
		}
		if err := d.AddEdge(id, ref.ID, map[string]any{AttributeOrderIndex: index}); err != nil {
			return fmt.Errorf("failed to add edge %v: %w", id, err)
		}
		refID := ref.ID
		wg.Go(func() error {
			if err := d.discover(ctx, refID, process, doneMap); err != nil {
				return fmt.Errorf("failed to discover reference %v: %w", id, err)
			}
			return nil
		})
	}
	if err = wg.Wait(); err != nil {
		vertex.Attributes.Store(AttributeDiscoveryState, StateError)
		return err
	}
	vertex.Attributes.Store(AttributeDiscoveryState, StateCompleted)
	return nil
}

// VertexMapRepresentation is a map-based representation of a vertex for easier testing and inspection.
type VertexMapRepresentation[T cmp.Ordered] struct {
	ID         T
	Attributes map[string]any
	Edges      map[T]map[string]any
}

// GraphMapRepresentation is a map-based representation of the entire graph, including degree maps.
type GraphMapRepresentation[T cmp.Ordered] struct {
	Vertices  map[T]*VertexMapRepresentation[T]
	OutDegree map[T]int
	InDegree  map[T]int
}

// ToMap converts the concurrent graph structure into a regular map-based
// structure for testing and non-concurrent evaluation.
func (d *DirectedAcyclicGraph[T]) ToMap() *GraphMapRepresentation[T] {
	vertices := make(map[T]*VertexMapRepresentation[T])
	d.Vertices.Range(func(key, value any) bool {
		id, ok := key.(T)
		if !ok {
			return true
		}
		v, ok := value.(*Vertex[T])
		if !ok {
			return true
		}
		vertices[id] = &VertexMapRepresentation[T]{
			ID:         v.ID,
			Attributes: v.AttributesToMap(),
			Edges:      v.EdgesToMap(),
		}
		return true
	})
	return &GraphMapRepresentation[T]{
		Vertices:  vertices,
		OutDegree: d.OutDegreeToMap(),
		InDegree:  d.InDegreeToMap(),
	}
}

// AttributesToMap converts the vertex sync.Map attributes to a regular map for
// easier testing and non-concurrent evaluation.
func (v *Vertex[T]) AttributesToMap() map[string]any {
	return SyncMapToMap[string, any](v.Attributes)
}

// EdgesToMap converts the vertex sync.Map edges and their attributes to
// regular maps for easier testing and evaluation.
func (v *Vertex[T]) EdgesToMap() map[T]map[string]any {
	edges := make(map[T]map[string]any)
	v.Edges.Range(func(key, value any) bool {
		if edgeID, ok := key.(T); ok {
			if attrMap, ok := value.(*sync.Map); ok {
				edges[edgeID] = SyncMapToMap[string, any](attrMap)
			}
		}
		return true
	})
	return edges
}

// VerticesToMap converts the graph's vertices sync.Map to a regular map for
// easier testing and non-concurrent evaluation.
func (d *DirectedAcyclicGraph[T]) VerticesToMap() map[T]*Vertex[T] {
	return SyncMapToMap[T, *Vertex[T]](d.Vertices)
}

// OutDegreeToMap converts the graph's out-degree sync.Map to a regular map for
// easier testing and non-concurrent evaluation.
func (d *DirectedAcyclicGraph[T]) OutDegreeToMap() map[T]int {
	return SyncMapToMap[T, int](d.OutDegree)
}

// InDegreeToMap converts the graph's in-degree sync.Map to a regular map for
// easier testing and non-concurrent evaluation.
func (d *DirectedAcyclicGraph[T]) InDegreeToMap() map[T]int {
	return SyncMapToMap[T, int](d.InDegree)
}

// SyncMapToMap converts a sync.Map to a regular map with type assertions.
// This is an auxiliary function to facilitate conversion of sync.Map in the
// graph structure to a regular map for easier testing and non-concurrent
// evaluation.
func SyncMapToMap[K comparable, V any](m *sync.Map) map[K]V {
	result := make(map[K]V)
	m.Range(func(key, value any) bool {
		if k, ok := key.(K); ok {
			if v, ok := value.(V); ok {
				result[k] = v
			} else {
				var zeroValue V
				slog.Error("Value type mismatch in sync.Map", "expected", fmt.Sprintf("%T", zeroValue), "got", fmt.Sprintf("%T", value))
			}
		}
		return true
	})
	return result
}
