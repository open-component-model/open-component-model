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
	type kv struct {
		Key   T
		Value *sync.Map
	}
	var kvSlice []kv

	vertex.Edges.Range(func(key, value any) bool {
		childId, ok1 := key.(T)
		attributes, ok2 := value.(*sync.Map)
		if ok1 && ok2 {
			kvSlice = append(kvSlice, kv{
				Key:   childId,
				Value: attributes,
			})
		}
		return true
	})

	// Sort kvSlice by order index if available, otherwise by key
	slices.SortFunc(kvSlice, func(a, b kv) int {
		var orderA, orderB int
		var okA, okB bool
		if oa, ok := a.Value.Load(syncdag.AttributeOrderIndex); ok {
			orderA, okA = oa.(int)
		}
		if ob, ok := b.Value.Load(syncdag.AttributeOrderIndex); ok {
			orderB, okB = ob.(int)
		}
		if okA && okB {
			return orderA - orderB
		} else if okA {
			return -1
		} else if okB {
			return 1
		}
		return cmp.Compare(a.Key, b.Key)
	})

	children := make([]T, len(kvSlice))
	for i, kv := range kvSlice {
		children[i] = kv.Key
	}
	return children
}
