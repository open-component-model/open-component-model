package sync

import (
	"cmp"
	"sync"

	"ocm.software/open-component-model/bindings/go/dag"
)

// ToSyncMapBasedDAG converts a map-based DAG to a sync.Map-based DAG.
func ToSyncMapBasedDAG[T cmp.Ordered](d *dag.DirectedAcyclicGraph[T]) *DirectedAcyclicGraph[T] {
	vertices := &sync.Map{}
	for id, v := range d.Vertices {
		vtx := &Vertex[T]{
			ID:         v.ID,
			Attributes: VertexAttributesToSyncMap(v),
			Edges:      VertexEdgesToSyncMap(v),
		}
		vtx.InDegree.Store(int64(v.InDegree))
		vtx.OutDegree.Store(int64(v.OutDegree))
		vertices.Store(id, vtx)
	}
	return &DirectedAcyclicGraph[T]{
		Vertices: vertices,
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

// MapToSyncMap converts a map to a sync.Map.
func MapToSyncMap[K comparable, V any](m map[K]V) *sync.Map {
	sm := &sync.Map{}
	for k, v := range m {
		sm.Store(k, v)
	}
	return sm
}
