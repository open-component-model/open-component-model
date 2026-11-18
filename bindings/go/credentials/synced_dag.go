package credentials

import (
	"errors"
	"fmt"
	"strings"
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

func newSyncedDag() *syncedDag {
	return &syncedDag{
		dag: dag.NewDirectedAcyclicGraph[string](),
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

func (g *syncedDag) getIdentity(id string) (runtime.Identity, bool) {
	v, ok := g.getVertex(id)
	if !ok {
		return nil, false
	}
	identity, ok := v.Attributes[attributeIdentity].(runtime.Identity)
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

// matchAnyNode attempts to locate the graph vertex corresponding to the provided node ID.
// If an exact match is not found, it falls back to a wildcard search by comparing identities
// using the Identity.Match method.
// This wildcard search is the reason there can be undiscovered cycles at runtime.
//
// When more than one vertex matches, matchAnyNode selects the most specific one
// deterministically: first the identity with the most attributes, then the
// lexicographically smallest identity string as a stable tie-break. This keeps
// resolution stable across runs even when overlapping entries (for example a
// hostname-only entry and the same host scoped to an explicit port or path)
// both match the same lookup.
func (g *syncedDag) matchAnyNode(identity runtime.Identity) (*dag.Vertex[string], error) {
	g.dagMu.RLock()
	defer g.dagMu.RUnlock()
	node := identity.String()
	if vertex, ok := g.dag.Vertices[node]; ok {
		return vertex, nil
	}

	var best *dag.Vertex[string]
	var bestIdentity runtime.Identity
	for _, vertex := range g.dag.Vertices {
		existing, ok := vertex.Attributes[attributeIdentity].(runtime.Identity)
		if !ok {
			continue
		}
		if !identity.Match(existing) {
			continue
		}
		if best == nil || moreSpecific(existing, bestIdentity) {
			best, bestIdentity = vertex, existing
		}
	}
	if best != nil {
		return best, nil
	}
	return nil, fmt.Errorf("failed to match any node: %w", ErrNoDirectCredentials)
}

// moreSpecific reports whether identity a is more specific than identity b.
//
// Specificity is ranked in this order:
//  1. An identity with more attributes is more specific (for example an entry
//     scoped to an explicit port or path beats a hostname-only entry).
//  2. For an equal attribute count, the identity whose path glob has fewer
//     wildcard characters is more specific (for example "my-org/*" beats "*/*").
//  3. As a final, stable tie-break, the lexicographically smaller identity
//     string is treated as more specific, so selection is deterministic.
func moreSpecific(a, b runtime.Identity) bool {
	if len(a) != len(b) {
		return len(a) > len(b)
	}
	if aw, bw := pathWildcards(a), pathWildcards(b); aw != bw {
		return aw < bw
	}
	return a.String() < b.String()
}

// pathWildcards counts the glob wildcard characters in the path attribute of an
// identity. An identity with no path, or a path with no wildcards, counts as 0.
func pathWildcards(i runtime.Identity) int {
	return strings.Count(i[runtime.IdentityAttributePath], "*")
}

// addIdentity ensures that a given identity is represented as a vertex in the graph.
// It also establishes edges between the new node and any existing nodes that match with each other.
func (g *syncedDag) addIdentity(identity runtime.Identity) error {
	g.dagMu.Lock()
	defer g.dagMu.Unlock()

	node := identity.String()
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
		existing, ok := vertex.Attributes[attributeIdentity].(runtime.Identity)
		if !ok {
			continue
		}
		if identity.Match(existing) {
			if err := g.dag.AddEdge(vertex.ID, node, map[string]any{
				"kind": "cyclic-only",
			}); err != nil {
				return err
			}
		}
		if existing.Match(identity) {
			if err := g.dag.AddEdge(node, vertex.ID, map[string]any{
				"kind": "cyclic-only",
			}); err != nil {
				return err
			}
		}
	}
	return nil
}
