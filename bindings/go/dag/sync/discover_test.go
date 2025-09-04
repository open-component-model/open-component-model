package sync

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDAGDiscovery(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)

	t.Run("graph discovery succeeds", func(t *testing.T) {
		// Emulate external dependency graph.
		// In real-world scenarios, this information would likely be retrieved from
		// an API (such as an OCM Repository).
		graph := map[string][]string{
			"A": {"B", "C"},
			"B": {"D"},
			"C": {"D"},
			"D": {},
		}

		dag := NewDirectedAcyclicGraph[string]()
		discoverFunc := func(ctx context.Context, v string) ([]string, error) {
			// Simulate fetching dependencies from an external graph.
			// In a real-world scenario, this would likely be an API call (such
			// as OCM GetComponentVersion)
			dep, ok := graph[v]
			if !ok {
				return nil, fmt.Errorf("no node found with ID %s", v)
			}
			var neighbors []string
			for _, id := range dep {
				neighbors = append(neighbors, id)
			}
			return neighbors, nil
		}
		// Start the discovery from multiple roots
		r.NoError(dag.Discover(ctx, DiscoverNeighborsFunc[string](discoverFunc), WithRoots[string]("A", "B", "C", "D")))

		// Check if the graph structure is as expected
		r.ElementsMatchf(dag.MustGetVertex("A").EdgeKeys(), []string{"B", "C"}, "expected edges from A to B and C, but got %v", dag.MustGetVertex("A").EdgeKeys())
		r.ElementsMatchf(dag.MustGetVertex("B").EdgeKeys(), []string{"D"}, "expected edge from B to D, but got %v", dag.MustGetVertex("B").EdgeKeys())
		r.ElementsMatchf(dag.MustGetVertex("C").EdgeKeys(), []string{"D"}, "expected edge from C to D, but got %v", dag.MustGetVertex("C").EdgeKeys())
		r.ElementsMatchf(dag.MustGetVertex("D").EdgeKeys(), []string{}, "expected no edges from D, but got %v", dag.MustGetVertex("D").EdgeKeys())

		// As Discover uses addRawVertex and AddEdge internally, which are unit tested
		// separately, we can assume out degree and in degree are correct if the
		// graph structure is correct.
	})

	t.Run("graph discovery fails with canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		// Simulate a context cancellation
		cancel()

		dag := NewDirectedAcyclicGraph[string]()
		discoveryFunc := func(ctx context.Context, v string) ([]string, error) {
			return nil, fmt.Errorf("we should never reach this point due to context cancellation")
		}

		err := dag.Discover(ctx, DiscoverNeighborsFunc[string](discoveryFunc), WithRoots("A"))
		r.ErrorIsf(err, context.Canceled, "expected error due to context cancellation, but got nil")
	})

	t.Run("graph discovery fails in discovery function", func(t *testing.T) {
		// Emulate an invalid external dependency graph. Here, the edge C -> D
		// exists, but D is not found in the graph.
		graph := map[string][]string{
			"A": {"B", "C"},
			"B": {},
			"C": {"D"},
		}
		dag := NewDirectedAcyclicGraph[string]()
		discoveryFunc := func(ctx context.Context, v string) ([]string, error) {
			// Simulate fetching dependencies from an external graph.
			// In a real-world scenario, this would likely be an API call (such
			// as OCM GetComponentVersion)
			dep, ok := graph[v]
			if !ok {
				return nil, fmt.Errorf("no node found with ID %s", v)
			}
			var neighbors []string
			for _, id := range dep {
				neighbors = append(neighbors, id)
			}
			return neighbors, nil
		}

		err := dag.Discover(ctx, DiscoverNeighborsFunc[string](discoveryFunc), WithRoots("A"), WithGoRoutineLimit[string](1))
		r.Error(err, "expected error due to missing node in the external graph, but got nil")

		r.Equal(dag.MustGetVertex("A").MustGetAttribute(AttributeDiscoveryState), StateError, "expected vertex A to be in error state, but got %s", dag.MustGetVertex("A").MustGetAttribute(AttributeDiscoveryState))
		r.Equal(dag.MustGetVertex("B").MustGetAttribute(AttributeDiscoveryState), StateCompleted, "expected vertex B to be in completed state, but got %s", dag.MustGetVertex("B").MustGetAttribute(AttributeDiscoveryState))
		r.Equal(dag.MustGetVertex("C").MustGetAttribute(AttributeDiscoveryState), StateError, "expected vertex C to be in error state, but got %s", dag.MustGetVertex("C").MustGetAttribute(AttributeDiscoveryState))
	})
}
