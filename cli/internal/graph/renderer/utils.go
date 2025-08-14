package renderer

import (
	"cmp"
	"slices"
	"sync"

	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
)

// GetNeighborsSorted returns the neighbors of the given vertex sorted by their
// order index if available, otherwise by their key.
// This function may be used to implement GraphRenderer with a consistent
// order of neighbors in the output.
func GetNeighborsSorted[T cmp.Ordered](vertex *syncdag.Vertex[T]) []T {
	var neighbors []T

	vertex.Edges.Range(func(key, value any) bool {
		if childId, ok := key.(T); ok {
			neighbors = append(neighbors, childId)
		}
		return true
	})

	slices.SortFunc(neighbors, func(a, b T) int {
		return compareByOrderIndex(vertex, a, b)
	})

	return neighbors
}

func compareByOrderIndex[T cmp.Ordered](vertex *syncdag.Vertex[T], a, b T) int {
	orderA := getOrderIndex(vertex, a)
	orderB := getOrderIndex(vertex, b)

	if orderA != nil && orderB != nil {
		return *orderA - *orderB
	}
	if orderA != nil {
		return -1
	}
	if orderB != nil {
		return 1
	}
	return cmp.Compare(a, b)
}

func getOrderIndex[T cmp.Ordered](vertex *syncdag.Vertex[T], key T) *int {
	if value, exists := vertex.Edges.Load(key); exists {
		if attrs, ok := value.(*sync.Map); ok {
			if order, exists := attrs.Load(syncdag.AttributeOrderIndex); exists {
				if orderInt, ok := order.(int); ok {
					return &orderInt
				}
			}
		}
	}
	return nil
}
