package sync

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/dag"
)

func discoverEdges(ctx context.Context, edges map[string][]string) error {
	d := NewGraphDiscoverer(&GraphDiscovererOptions[string, string]{
		Roots:      []string{"a"},
		Resolver:   ResolverFunc[string, string](func(_ context.Context, k string) (string, error) { return k, nil }),
		Discoverer: DiscovererFunc[string, string](func(_ context.Context, v string) ([]string, error) { return edges[v], nil }),
	})

	return d.Discover(ctx)
}

// TestDiscoverReportsCycles pins that a cyclic key graph fails fast with the
// DAG's own cycle error. Edges are added before descending precisely so
// AddEdge's cycle check sees a back-edge to an in-flight ancestor; adding them
// after the recursion makes the check unreachable and the traversal blocks on
// the ancestor's done channel until the context is cancelled.
func TestDiscoverReportsCycles(t *testing.T) {
	// dag.HasCycle uses a map, so the reported path is an arbitrary rotation of the same.
	// same as dag_test.go:159-166.
	for _, tc := range []struct {
		name     string
		edges    map[string][]string
		rotation [][]string
	}{
		{
			"two cycle",
			map[string][]string{"a": {"b"}, "b": {"a"}},
			[][]string{
				{"a", "b", "a"},
				{"b", "a", "b"},
			},
		},
		{
			"three cycle",
			map[string][]string{"a": {"b"}, "b": {"c"}, "c": {"a"}},
			[][]string{
				{"a", "b", "c", "a"},
				{"b", "c", "a", "b"},
				{"c", "a", "b", "c"},
			},
		},
		{
			"back edge to root from a deeper branch",
			map[string][]string{"a": {"b", "c"}, "c": {"d"}, "d": {"a"}},
			[][]string{
				{"a", "c", "d", "a"},
				{"c", "d", "a", "c"},
				{"d", "a", "c", "d"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()

			start := time.Now()
			err := discoverEdges(ctx, tc.edges)

			r.Error(err)
			r.NotErrorIs(err, context.DeadlineExceeded, "a cycle must fail, not hang")
			r.Less(time.Since(start), time.Second, "a cycle must fail fast")

			var cycleErr *dag.CycleError
			r.ErrorAs(err, &cycleErr)
			r.Contains(tc.rotation, cycleErr.Cycle)
		})
	}
}

// TestDiscoverReportsSelfReference covers the degenerate cycle, which also used
// to hang: the child goroutine waited on its own parent's done channel.
func TestDiscoverReportsSelfReference(t *testing.T) {
	r := require.New(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	err := discoverEdges(ctx, map[string][]string{"a": {"a"}})
	r.Error(err)
	r.ErrorIs(err, dag.ErrSelfReference)
}

// TestDiscoverAcyclicReconvergence guards the other direction: a vertex reached
// by two distinct paths is legitimate and must still resolve exactly once.
func TestDiscoverAcyclicReconvergence(t *testing.T) {
	r := require.New(t)

	var mu sync.Mutex
	resolved := map[string]int{}

	d := NewGraphDiscoverer(&GraphDiscovererOptions[string, string]{
		Roots: []string{"a"},
		Resolver: ResolverFunc[string, string](func(_ context.Context, k string) (string, error) {
			mu.Lock()
			resolved[k]++
			mu.Unlock()

			return k, nil
		}),
		Discoverer: DiscovererFunc[string, string](func(_ context.Context, v string) ([]string, error) {
			return map[string][]string{"a": {"b", "c"}, "b": {"d"}, "c": {"d"}}[v], nil
		}),
	})
	r.NoError(d.Discover(t.Context()))

	for _, key := range []string{"a", "b", "c", "d"} {
		r.Equal(1, resolved[key], "vertex %q must resolve exactly once", key)
	}
	r.Equal([]string{"b", "c"}, d.CurrentEdges("a"))
	r.Equal([]string{"d"}, d.CurrentEdges("b"))
	r.Equal([]string{"d"}, d.CurrentEdges("c"))
}
