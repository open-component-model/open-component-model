package credentials

import (
	"errors"
	"fmt"
	"sync"

	"ocm.software/open-component-model/bindings/go/dag"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	attributeIdentity = "attributes.ocm.software/identity"
	//nolint:gosec // gosec thinks this is a hardcoded credential, but it's not.
	attributeCredentials = "attributes.ocm.software/credentials"
)

// ErrNoDirectCredentials is returned when a node in the graph does not have any directly
// attached credentials. There might still be credentials available through
// plugins which can be resolved at runtime.
var ErrNoDirectCredentials = errors.New("no direct credentials found in graph")

// WithCycleDetection enables cycle detection using Tarjan's algorithm for the DAG.
// This provides O(V+E) time complexity for cycle detection compared to the default DFS approach.
func WithCycleDetection() dag.Option[string] {
	return func(g *dag.DirectedAcyclicGraph[string]) {
		// The cycle detection is now automatically used by the HasCycle() method
		// No additional setup needed as it's integrated into the DAG implementation
	}
}

func newSyncedDag(opts ...dag.Option[string]) *syncedDag {
	d := dag.NewDirectedAcyclicGraph[string](opts...)
	return &syncedDag{
		dag: d,
	}
}

type syncedDag struct {
	dagMu sync.RWMutex
	dag   *dag.DirectedAcyclicGraph[string]
}

func (g *syncedDag) getVertex(id string) (v *dag.Vertex[string], ok bool) {
	g.dagMu.RLock()
	defer g.dagMu.RUnlock()
	v, ok = g.dag.Vertices[id]
	return v, ok
}

func (g *syncedDag) getIdentity(id string) (runtime.Typed, bool) {
	v, ok := g.getVertex(id)
	if !ok {
		return nil, false
	}
	identity, ok := v.Attributes[attributeIdentity].(runtime.Typed)
	return identity, ok
}

func (g *syncedDag) getCredentials(id string) (runtime.Typed, bool) {
	v, ok := g.getVertex(id)
	if !ok {
		return nil, false
	}
	credentials, ok := v.Attributes[attributeCredentials].(runtime.Typed)
	return credentials, ok
}

func (g *syncedDag) setCredentials(id string, credentials runtime.Typed) {
	g.dagMu.Lock()
	defer g.dagMu.Unlock()
	v, ok := g.dag.Vertices[id]
	if !ok {
		return
	}
	v.Attributes[attributeCredentials] = credentials
}

func (g *syncedDag) addEdge(from, to string, attributes ...map[string]any) error {
	g.dagMu.Lock()
	defer g.dagMu.Unlock()
	return g.dag.AddEdge(from, to, attributes...)
}

// nodeID returns a stable string identifier for a runtime.Typed identity.
// Used as the DAG vertex key and cache key throughout the graph.
//
// Typed identity structs MUST implement fmt.Stringer to produce stable, deterministic
// node IDs. The fallback fmt.Sprintf("%v", typed) is not guaranteed to be stable
// (e.g. pointer addresses for pointer fields). runtime.Identity satisfies this
// via sorted key=value pairs.
func nodeID(typed runtime.Typed) string {
	if s, ok := typed.(fmt.Stringer); ok {
		return s.String()
	}
	return fmt.Sprintf("%v", typed)
}

// typedMatch checks whether identity a matches identity b.
// Currently delegates to runtime.Identity.Match when both sides are Identity maps.
// Returns false for non-Identity types until matching is generalized.
//
// This will only work with runtime.Identity/map identities.
// A panic will be thrown if runtime.Typed is something else.
// See the tracking issue https://github.com/open-component-model/ocm-project/issues/1041
func typedMatch(a, b runtime.Typed) bool {
	idA, okA := a.(runtime.Identity)
	if !okA {
		panic("a must be of type runtime.Identity")
	}
	idB, okB := b.(runtime.Identity)
	if !okB {
		panic("b must be of type runtime.Identity")
	}
	return idA.Match(idB)
}

// matchAnyNode attempts to locate the graph vertex corresponding to the provided identity.
// If an exact match is not found, it falls back to a wildcard search using typedMatch.
// This wildcard search is the reason there can be undiscovered cycles at runtime.
func (g *syncedDag) matchAnyNode(identity runtime.Typed) (*dag.Vertex[string], error) {
	g.dagMu.RLock()
	defer g.dagMu.RUnlock()
	node := nodeID(identity)
	if vertex, ok := g.dag.Vertices[node]; ok {
		return vertex, nil
	}
	for _, vertex := range g.dag.Vertices {
		existing, ok := vertex.Attributes[attributeIdentity].(runtime.Typed)
		if !ok {
			continue
		}
		if typedMatch(identity, existing) {
			return vertex, nil
		}
	}
	return nil, fmt.Errorf("failed to match any node: %w", ErrNoDirectCredentials)
}

// addIdentity ensures that a given identity is represented as a vertex in the graph.
// It also establishes edges between the new node and any existing nodes that match with each other.
func (g *syncedDag) addIdentity(identity runtime.Typed) error {
	g.dagMu.Lock()
	defer g.dagMu.Unlock()

	node := nodeID(identity)
	if g.dag.Contains(node) {
		return nil
	}
	if err := g.dag.AddVertex(node, map[string]any{
		attributeIdentity: identity,
	}); err != nil {
		return err
	}
	for _, vertex := range g.dag.Vertices {
		if vertex.ID == node {
			continue
		}
		existing, ok := vertex.Attributes[attributeIdentity].(runtime.Typed)
		if !ok {
			continue
		}
		if typedMatch(identity, existing) {
			if err := g.dag.AddEdge(vertex.ID, node, map[string]any{
				"kind": "cyclic-only",
			}); err != nil {
				return err
			}
		}
		if typedMatch(existing, identity) {
			if err := g.dag.AddEdge(node, vertex.ID, map[string]any{
				"kind": "cyclic-only",
			}); err != nil {
				return err
			}
		}
	}
	return nil
}
