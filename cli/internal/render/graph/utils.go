package graph

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"ocm.software/open-component-model/bindings/go/dag"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
)

// GetNeighborsSorted returns the neighbors of the given vertex sorted by their
// order index if available, otherwise by their key.
// This function may be used to implement Renderer with a consistent
// order of neighbors in the output.
func GetNeighborsSorted[T cmp.Ordered](ctx context.Context, vertex *dag.Vertex[T]) ([]T, error) {
	var neighbors []T

	for childId := range vertex.Edges {
		neighbors = append(neighbors, childId)
	}

	var err error
	slices.SortFunc(neighbors, func(edgeIdA, edgeIdB T) int {
		index, compareErr := compareByOrderIndex(ctx, vertex, edgeIdA, edgeIdB)
		err = errors.Join(err, compareErr)
		return index
	})
	if err != nil {
		return nil, fmt.Errorf("failed to sort neighbors of vertex %v: %w", vertex.ID, err)
	}

	return neighbors, nil
}

// compareByOrderIndex compares two edges.
// If the AttributeOrderIndex is set on the edges with edgeIdA and edgeIdB,
// this function compares the order indices and returns the
// difference (i.e. edgeA.Index - edgeB.Index).
// If the order index is not set on one of both edges, it falls back to
// comparing the edge IDs.
func compareByOrderIndex[T cmp.Ordered](ctx context.Context, vertex *dag.Vertex[T], edgeIdA, edgeIdB T) (int, error) {
	orderA, err := getOrderIndex(ctx, vertex, edgeIdA)
	if err != nil {
		return 0, fmt.Errorf("failed to get order index for edge from %v to %v: %w", vertex.ID, edgeIdA, err)
	}
	orderB, err := getOrderIndex(ctx, vertex, edgeIdB)
	if err != nil {
		return 0, fmt.Errorf("failed to get order index for edge from %v to %v: %w", vertex.ID, edgeIdB, err)
	}

	// If both edges have order indices, compare them.
	if orderA != nil && orderB != nil {
		return cmp.Compare(*orderA, *orderB), nil
	}
	// If one of the order indices is nil, we cannot compare the order indexes
	// and compare by the IDs directly.
	return cmp.Compare(edgeIdA, edgeIdB), nil
}

// getOrderIndex retrieves the value of AttributeOrderIndex for the given
// edgeId.
func getOrderIndex[T cmp.Ordered](_ context.Context, vertex *dag.Vertex[T], key T) (*int, error) {
	edge, ok := vertex.Edges[key]
	if !ok {
		return nil, fmt.Errorf("vertex %v does not have an edge to %v", vertex.ID, key)
	}
	orderIndex, ok := edge[syncdag.AttributeOrderIndex]
	if !ok {
		// Order index not being set is acceptable. The render logic is
		// supposed to be able to handle graphs not constructed by our discovery
		// logic that might therefore not have that attribute set.
		return nil, nil
	}
	order, ok := orderIndex.(int)
	if !ok {
		return nil, fmt.Errorf("edge from vertex %v to %v has an attribute %s of unexpected type %T, expected type %T", vertex.ID, key, syncdag.AttributeOrderIndex, orderIndex, 0)
	}
	return &order, nil
}
