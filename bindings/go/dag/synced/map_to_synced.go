package synced

import (
	"cmp"
	"sync"

	"ocm.software/open-component-model/bindings/go/dag"
)

// ToSyncMapBasedDAG converts a map-based DAG to a sync.Map-based DAG.
func ToSyncMapBasedDAG[T cmp.Ordered](d *dag.DirectedAcyclicGraph[T]) *DirectedAcyclicGraph[T] {
	vertices := &sync.Map{}
	for id, v := range d.Vertices {
		vertices.Store(id, &Vertex[T]{
			ID:         v.ID,
			Attributes: VertexAttributesToSyncMap(v),
			Edges:      VertexEdgesToSyncMap(v),
		})
	}
	return &DirectedAcyclicGraph[T]{
		Vertices:  vertices,
		OutDegree: OutDegreeToSyncMap(d),
		InDegree:  InDegreeToSyncMap(d),
	}
}

func VertexAttributesToSyncMap[T cmp.Ordered](v *dag.Vertex[T]) *sync.Map {
	return MapToSyncMap[string, any](v.Attributes)
}

func VertexEdgesToSyncMap[T cmp.Ordered](v *dag.Vertex[T]) *sync.Map {
	edges := &sync.Map{}
	for edgeID, attrMap := range v.Edges {
		edges.Store(edgeID, MapToSyncMap[string, any](attrMap))
	}
	return edges
}

func OutDegreeToSyncMap[T cmp.Ordered](d *dag.DirectedAcyclicGraph[T]) *sync.Map {
	return MapToSyncMap[T, int](d.OutDegree)
}

func InDegreeToSyncMap[T cmp.Ordered](d *dag.DirectedAcyclicGraph[T]) *sync.Map {
	return MapToSyncMap[T, int](d.InDegree)
}

// MapToSyncMap converts a map to a sync.Map.
func MapToSyncMap[K comparable, V any](m map[K]V) *sync.Map {
	sm := &sync.Map{}
	for k, v := range m {
		sm.Store(k, v)
	}
	return sm
}
