package sync

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/dag"
)

func TestMapToSynced(t *testing.T) {
	r := require.New(t)

	mapBasedDAG := dag.NewDirectedAcyclicGraph[string]()
	r.NoError(mapBasedDAG.AddVertex("A", map[string]any{"key": "1"}))
	r.NoError(mapBasedDAG.AddVertex("B", map[string]any{"key": "2"}))
	r.NoError(mapBasedDAG.AddVertex("C", map[string]any{"key": "3"}))

	r.NoError(mapBasedDAG.AddEdge("A", "B", map[string]any{"key": "1"}))
	r.NoError(mapBasedDAG.AddEdge("B", "C", map[string]any{"key": "2"}))

	syncMapBasedDAG := NewDirectedAcyclicGraph[string]()
	r.NoError(syncMapBasedDAG.AddVertex("A", map[string]any{"key": "1"}))
	r.NoError(syncMapBasedDAG.AddVertex("B", map[string]any{"key": "2"}))
	r.NoError(syncMapBasedDAG.AddVertex("C", map[string]any{"key": "3"}))

	r.NoError(syncMapBasedDAG.AddEdge("A", "B", map[string]any{"key": "1"}))
	r.NoError(syncMapBasedDAG.AddEdge("B", "C", map[string]any{"key": "2"}))

	var compare = func(syncMapBasedDAG *DirectedAcyclicGraph[string], mapBasedDAG *dag.DirectedAcyclicGraph[string]) {
		r.Equalf(syncMapBasedDAG.LengthVertices(), len(mapBasedDAG.Vertices), "expected number of vertices to match, but they differ")

		r.Equalf(syncMapBasedDAG.MustGetVertex("A").LengthEdges(), len(mapBasedDAG.Vertices["A"].Edges), "expected vertex A to have the same number of edges, but they differ")
		r.Equalf(syncMapBasedDAG.MustGetVertex("B").LengthEdges(), len(mapBasedDAG.Vertices["B"].Edges), "expected vertex B to have the same number of edges, but they differ")
		r.Equalf(syncMapBasedDAG.MustGetVertex("C").LengthEdges(), len(mapBasedDAG.Vertices["C"].Edges), "expected vertex C to have the same number of edges, but they differ")

		r.Equalf(syncMapBasedDAG.MustGetVertex("A").MustGetAttribute("key"), mapBasedDAG.Vertices["A"].Attributes["key"], "expected vertex A attribute 'key' to match, but they differ")
		r.Equalf(syncMapBasedDAG.MustGetVertex("B").MustGetAttribute("key"), mapBasedDAG.Vertices["B"].Attributes["key"], "expected vertex B attribute 'key' to match, but they differ")
		r.Equalf(syncMapBasedDAG.MustGetVertex("C").MustGetAttribute("key"), mapBasedDAG.Vertices["C"].Attributes["key"], "expected vertex C attribute 'key' to match, but they differ")

		r.Equalf(syncMapBasedDAG.MustGetVertex("A").MustGetEdgeAttribute("B", "key"), mapBasedDAG.Vertices["A"].Edges["B"]["key"], "expected edge A->B attribute 'key' to match, but they differ")
		r.Equalf(syncMapBasedDAG.MustGetVertex("B").MustGetEdgeAttribute("C", "key"), mapBasedDAG.Vertices["B"].Edges["C"]["key"], "expected edge B->C attribute 'key' to match, but they differ")

		r.ElementsMatchf(syncMapBasedDAG.MustGetVertex("A").EdgeKeys(), slices.Collect(maps.Keys(mapBasedDAG.Vertices["A"].Edges)), "expected vertex A to match, but they differ")
		r.ElementsMatchf(syncMapBasedDAG.MustGetVertex("B").EdgeKeys(), slices.Collect(maps.Keys(mapBasedDAG.Vertices["B"].Edges)), "expected vertex B to match, but they differ")
		r.ElementsMatchf(syncMapBasedDAG.MustGetVertex("C").EdgeKeys(), slices.Collect(maps.Keys(mapBasedDAG.Vertices["C"].Edges)), "expected vertex C to match, but they differ")

		r.Equalf(int(syncMapBasedDAG.MustGetOutDegree("A").Load()), mapBasedDAG.Vertices["A"].OutDegree, "expected out-degree to match, but they differ")
		r.Equalf(int(syncMapBasedDAG.MustGetOutDegree("B").Load()), mapBasedDAG.Vertices["B"].OutDegree, "expected out-degree to match, but they differ")
		r.Equalf(int(syncMapBasedDAG.MustGetOutDegree("C").Load()), mapBasedDAG.Vertices["C"].OutDegree, "expected out-degree to match, but they differ")

		r.Equalf(int(syncMapBasedDAG.MustGetInDegree("A").Load()), mapBasedDAG.Vertices["A"].InDegree, "expected in-degree to match, but they differ")
		r.Equalf(int(syncMapBasedDAG.MustGetInDegree("B").Load()), mapBasedDAG.Vertices["B"].InDegree, "expected in-degree to match, but they differ")
		r.Equalf(int(syncMapBasedDAG.MustGetInDegree("C").Load()), mapBasedDAG.Vertices["C"].InDegree, "expected in-degree to match, but they differ")

		r.Equalf(syncMapBasedDAG.Roots(), mapBasedDAG.Roots(), "expected roots to match, but they differ")
	}

	compare(syncMapBasedDAG, mapBasedDAG)
	compare(ToSyncMapBasedDAG(mapBasedDAG), mapBasedDAG)
	compare(syncMapBasedDAG, ToMapBasedDAG(syncMapBasedDAG))
}
