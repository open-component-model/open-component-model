package introspection

import (
	"cmp"
	"fmt"
	"log/slog"
	"sync"

	"ocm.software/open-component-model/bindings/go/dag"
)

// VertexMapSnapshot is a map-based representation.
type VertexMapSnapshot[T cmp.Ordered] struct {
	ID         T
	Attributes map[string]any
	Edges      map[T]map[string]any
}

// GraphMapSnapshot is a map-based representation of the entire graph.
type GraphMapSnapshot[T cmp.Ordered] struct {
	Vertices  map[T]*VertexMapSnapshot[T]
	OutDegree map[T]int
	InDegree  map[T]int
}

// ToMap converts the concurrent graph structure into a regular map-based.
func ToMap[T cmp.Ordered](d *dag.DirectedAcyclicGraph[T]) *GraphMapSnapshot[T] {
	vertices := make(map[T]*VertexMapSnapshot[T])
	d.Vertices.Range(func(key, value any) bool {
		id, ok := key.(T)
		if !ok {
			return true
		}
		v, ok := value.(*dag.Vertex[T])
		if !ok {
			return true
		}
		vertices[id] = &VertexMapSnapshot[T]{
			ID:         v.ID,
			Attributes: VertexAttributesToMap(v),
			Edges:      VertexEdgesToMap(v),
		}
		return true
	})
	return &GraphMapSnapshot[T]{
		Vertices:  vertices,
		OutDegree: OutDegreeToMap(d),
		InDegree:  InDegreeToMap(d),
	}
}

// VertexAttributesToMap converts the vertex sync.Map attributes to a regular
// map.
func VertexAttributesToMap[T cmp.Ordered](v *dag.Vertex[T]) map[string]any {
	return SyncMapToMap[string, any](v.Attributes)
}

// VertexEdgesToMap converts the vertex sync.Map edges and their attributes to
// regular maps.
func VertexEdgesToMap[T cmp.Ordered](v *dag.Vertex[T]) map[T]map[string]any {
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

// VerticesToMap converts the graph's vertices sync.Map to a regular map.
func VerticesToMap[T cmp.Ordered](d *dag.DirectedAcyclicGraph[T]) map[T]*dag.Vertex[T] {
	return SyncMapToMap[T, *dag.Vertex[T]](d.Vertices)
}

// OutDegreeToMap converts the graph's out-degree sync.Map to a regular.
func OutDegreeToMap[T cmp.Ordered](d *dag.DirectedAcyclicGraph[T]) map[T]int {
	return SyncMapToMap[T, int](d.OutDegree)
}

// InDegreeToMap converts the graph's in-degree sync.Map to a regular map.
func InDegreeToMap[T cmp.Ordered](d *dag.DirectedAcyclicGraph[T]) map[T]int {
	return SyncMapToMap[T, int](d.InDegree)
}

// SyncMapToMap converts a sync.Map to a regular map with type assertions.
// This is an auxiliary function to facilitate conversion of sync.Map in the
// graph structure to a regular map.
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
