package sync

import (
	"cmp"
	"fmt"
	"sync"

	"ocm.software/open-component-model/bindings/go/dag"
)

// ToMapBasedDAG converts the concurrent graph structure into a regular map-based.
func ToMapBasedDAG[T cmp.Ordered](d *DirectedAcyclicGraph[T]) *dag.DirectedAcyclicGraph[T] {
	vertices := make(map[T]*dag.Vertex[T])
	d.Vertices.Range(func(key, value any) bool {
		id, ok := key.(T)
		if !ok {
			return true
		}
		v, ok := value.(*Vertex[T])
		if !ok {
			return true
		}
		vtx := &dag.Vertex[T]{
			ID:         v.ID,
			Attributes: VertexAttributesToMap(v),
			Edges:      VertexEdgesToMap(v),
			InDegree:   int(v.InDegree.Load()),
			OutDegree:  int(v.OutDegree.Load()),
		}
		vertices[id] = vtx
		return true
	})
	return &dag.DirectedAcyclicGraph[T]{
		Vertices: vertices,
	}
}

// VertexAttributesToMap converts the vertex sync.Map attributes to a regular
// map.
func VertexAttributesToMap[T cmp.Ordered](v *Vertex[T]) map[string]any {
	return MustSyncMapToMap[string, any](v.Attributes)
}

// VertexEdgesToMap converts the vertex sync.Map edges and their attributes to
// regular maps.
func VertexEdgesToMap[T cmp.Ordered](v *Vertex[T]) map[T]map[string]any {
	edges := make(map[T]map[string]any)
	v.Edges.Range(func(key, value any) bool {
		if edgeID, ok := key.(T); ok {
			if attrMap, ok := value.(*sync.Map); ok {
				edges[edgeID] = MustSyncMapToMap[string, any](attrMap)
			}
		}
		return true
	})
	return edges
}

// SyncMapToMap converts a sync.Map to a regular map with type assertions.
// This is an auxiliary function to facilitate conversion of sync.Map in the
// graph structure to a regular map.
func SyncMapToMap[K comparable, V any](m *sync.Map) (map[K]V, error) {
	result := make(map[K]V)
	var err error
	m.Range(func(key, value any) bool {
		if k, ok := key.(K); ok {
			if v, ok := value.(V); ok {
				result[k] = v
			} else {
				var zeroValue V
				err = fmt.Errorf("value type mismatch in sync.Map, expected %T, got %T", zeroValue, value)
				return false
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func MustSyncMapToMap[K comparable, V any](m *sync.Map) map[K]V {
	result, err := SyncMapToMap[K, V](m)
	if err != nil {
		panic("failed to convert sync.Map to map: " + err.Error())
	}
	return result
}
