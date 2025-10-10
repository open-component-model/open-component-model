package graph

import (
	"cmp"
	"context"
	"slices"

	"ocm.software/open-component-model/bindings/go/dag"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
)

// GetNeighborsSorted returns the neighbors of the given vertex sorted by their
// order index if available, otherwise by their key.
// This function may be used to implement Renderer with a consistent
// order of neighbors in the output.
func GetNeighborsSorted[T cmp.Ordered](ctx context.Context, vertex *dag.Vertex[T]) []T {
	var neighbors []T

	for childId := range vertex.Edges {
		neighbors = append(neighbors, childId)
	}

	slices.SortFunc(neighbors, func(edgeIdA, edgeIdB T) int {
		return compareByOrderIndex(ctx, vertex, edgeIdA, edgeIdB)
	})

	return neighbors
}

// compareByOrderIndex compares two edges.
// If the AttributeOrderIndex is set on the edges with edgeIdA and edgeIdB,
// this function compares the order indices and returns the
// difference (i.e. edgeA.Index - edgeB.Index).
// If the order index is not set on one of both edges, it falls back to
// comparing the edge IDs.
func compareByOrderIndex[T cmp.Ordered](ctx context.Context, vertex *dag.Vertex[T], edgeIdA, edgeIdB T) int {
	orderA := getOrderIndex(ctx, vertex, edgeIdA)
	orderB := getOrderIndex(ctx, vertex, edgeIdB)

	// If both edges have order indices, compare them.
	if orderA != nil && orderB != nil {
		return cmp.Compare(*orderA, *orderB)
	}
	// If one of the order indices is nil, we cannot compare the order indexes
	// and compare by the IDs directly.
	return cmp.Compare(edgeIdA, edgeIdB)
}

// getOrderIndex retrieves the value of AttributeOrderIndex for the given
// edgeId.
func getOrderIndex[T cmp.Ordered](_ context.Context, vertex *dag.Vertex[T], key T) *int {
	edge, ok := vertex.Edges[key]
	if !ok {
		return nil
	}
	orderIndex, ok := edge[syncdag.AttributeOrderIndex]
	if !ok {
		return nil
	}
	order, ok := orderIndex.(int)
	if !ok {
		return nil
	}
	return &order
}
