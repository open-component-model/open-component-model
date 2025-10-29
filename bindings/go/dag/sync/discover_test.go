package sync

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDAGDiscovery(t *testing.T) {

	t.Run("graph discovery succeeds", func(t *testing.T) {
		ctx := t.Context()
		r := require.New(t)
		// Emulate external dependency graph.
		// In real-world scenarios, this information would likely be retrieved from
		// an API (such as an OCM Repository).
		graph := map[string][]string{
			"A": {"B", "C"},
			"B": {"D"},
			"C": {"D"},
			"D": {},
		}

		dag := NewGraphDiscoverer(&GraphDiscovererOptions[string, string]{
			Roots: []string{"A", "B", "C", "D"},
			Resolver: ResolverFunc[string, string](func(ctx context.Context, key string) (value string, err error) {
				// Simulate fetching a node from an external repository.
				// In a real-world scenario, this would likely be an API call (such
				// as OCM GetComponentVersion)
				if _, ok := graph[key]; !ok {
					return "", fmt.Errorf("no node found with ID %s", key)
				}
				return key, nil
			}),
			Discoverer: DiscovererFunc[string, string](func(ctx context.Context, parent string) (children []string, err error) {
				// simulate evaluating dependencies for a parent node.
				dep, ok := graph[parent]
				if !ok {
					return nil, fmt.Errorf("no node found with ID %s", parent)
				}
				var neighbors []string
				for _, id := range dep {
					neighbors = append(neighbors, id)
				}
				return neighbors, nil
			}),
		})
		// Start the discovery from multiple roots
		r.NoError(dag.Discover(ctx))

		// Check if the graph structure is as expected
		r.ElementsMatchf(dag.CurrentEdges("A"), []string{"B", "C"}, "expected edges from A to B and C, but got %v", dag.CurrentEdges("A"))
		r.ElementsMatchf(dag.CurrentEdges("B"), []string{"D"}, "expected edge from B to D, but got %v", dag.CurrentEdges("B"))
		r.ElementsMatchf(dag.CurrentEdges("C"), []string{"D"}, "expected edge from C to D, but got %v", dag.CurrentEdges("C"))
		r.ElementsMatchf(dag.CurrentEdges("D"), []string{}, "expected no edges from D, but got %v", dag.CurrentEdges("D"))

		// As Discover uses addRawVertex and AddEdge internally, which are unit tested
		// separately, we can assume out degree and in degree are correct if the
		// graph structure is correct.
	})

	t.Run("graph discovery fails with canceled context", func(t *testing.T) {
		ctx := t.Context()
		r := require.New(t)
		ctx, cancel := context.WithCancel(ctx)
		// Simulate a context cancellation
		cancel()

		dag := NewGraphDiscoverer(&GraphDiscovererOptions[string, string]{
			Roots: []string{"A"},
			Resolver: ResolverFunc[string, string](func(ctx context.Context, key string) (value string, err error) {

				return "", fmt.Errorf("we should never reach this point due to context cancellation")
			}),
		})

		err := dag.Discover(ctx)
		r.ErrorIsf(err, context.Canceled, "expected error due to context cancellation, but got nil")
	})

	t.Run("graph discovery fails in discovery function", func(t *testing.T) {
		ctx := t.Context()
		r := require.New(t)
		// Emulate an invalid external dependency graph. Here, the edge C -> D
		// exists, but D is not found in the graph.
		//    A
		//   / \
		//  B   C
		//   \ /
		//   (D)*
		graph := map[string][]string{
			"A": {"B", "C"},
			"C": {"D"},
		}
		dag := NewGraphDiscoverer(&GraphDiscovererOptions[string, string]{
			Roots: []string{"A"},
			Resolver: ResolverFunc[string, string](func(ctx context.Context, key string) (value string, err error) {
				if _, ok := graph[key]; !ok {
					return "", fmt.Errorf("no node found with ID %s", key)
				}
				return key, nil
			}),
			Discoverer: DiscovererFunc[string, string](func(ctx context.Context, parent string) (children []string, err error) {
				dep, ok := graph[parent]
				if !ok {
					return nil, fmt.Errorf("no node found with ID %s", parent)
				}
				var neighbors []string
				for _, id := range dep {
					neighbors = append(neighbors, id)
				}
				return neighbors, nil
			}),
		})

		err := dag.Discover(ctx)
		r.Error(err, "expected error due to missing node in the external graph, but got nil")

		aState := dag.CurrentState("A")
		r.Equal(DiscoveryStateError, aState, "expected vertex A to be in error state, but got %s", aState)

		// because of discovers property to abort early, if 2 nodes on the layer are running in parallel,
		// and one of them fails, the other one might be in an unknown state as it might not have yet been discovered.
		bState := dag.CurrentState("B")
		r.Contains([]DiscoveryState{DiscoveryStateError, DiscoveryStateUnknown}, bState, "expected vertex B to be in error state, but got %s", bState)
		cState := dag.CurrentState("C")
		r.Contains([]DiscoveryState{DiscoveryStateError, DiscoveryStateUnknown}, cState, "expected vertex C to be in error state, but got %s", cState)
	})

	t.Run("graph discovery fails in discovery function", func(t *testing.T) {
		ctx := t.Context()
		r := require.New(t)
		// Emulate an invalid external dependency graph. Here, the edge C -> D
		// exists, but D is not found in the graph.
		graph := map[string][]string{
			"B": {},
		}
		dag := NewGraphDiscoverer(&GraphDiscovererOptions[string, string]{
			Roots: []string{"B"},
			Resolver: ResolverFunc[string, string](func(ctx context.Context, key string) (value string, err error) {
				if _, ok := graph[key]; !ok {
					return "", fmt.Errorf("no node found with ID %s", key)
				}
				return key, nil
			}),
			Discoverer: DiscovererFunc[string, string](func(ctx context.Context, parent string) (children []string, err error) {
				dep, ok := graph[parent]
				if !ok {
					return nil, fmt.Errorf("no node found with ID %s", parent)
				}
				var neighbors []string
				for _, id := range dep {
					neighbors = append(neighbors, id)
				}
				return neighbors, nil
			}),
		})

		err := dag.Discover(ctx)
		r.NoError(err)

		r.Equal(dag.CurrentState("B"), DiscoveryStateCompleted, "expected vertex B to be in completed state, but got %s", dag.CurrentState("B"))
	})
}
