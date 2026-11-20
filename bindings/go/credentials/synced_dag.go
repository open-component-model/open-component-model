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
// It first tries an exact identity-string match and returns it immediately when found.
// If no exact match exists, it falls back to a wildcard search by comparing identities
// using the Identity.Match method.
// This wildcard search is the reason there can be undiscovered cycles at runtime.
//
// When more than one vertex matches in the wildcard fallback, matchAnyNode selects a
// single vertex deterministically so resolution is stable across runs even for
// overlapping entries (for example a hostname-only entry and the same host scoped to an
// explicit port or path). Selection order is:
//  1. A vertex that can supply credentials (it has cached credentials or at least one
//     non-cyclic resolution edge) is preferred over a dependency-only vertex, so a more
//     specific but credential-less vertex cannot starve a matching consumer.
//  2. Within that group the most specific identity wins, as defined by moreSpecific.
func (g *syncedDag) matchAnyNode(identity runtime.Identity) (*dag.Vertex[string], error) {
	g.dagMu.RLock()
	defer g.dagMu.RUnlock()
	node := identity.String()
	if vertex, ok := g.dag.Vertices[node]; ok {
		return vertex, nil
	}

	var best *dag.Vertex[string]
	var bestIdentity runtime.Identity
	var bestSupplies bool
	for _, vertex := range g.dag.Vertices {
		existing, ok := vertex.Attributes[attributeIdentity].(runtime.Identity)
		if !ok {
			continue
		}
		if !identity.Match(existing) {
			continue
		}
		supplies := g.canSupplyCredentials(vertex)
		if best == nil || preferMatch(supplies, existing, bestSupplies, bestIdentity) {
			best, bestIdentity, bestSupplies = vertex, existing, supplies
		}
	}
	if best != nil {
		return best, nil
	}
	return nil, fmt.Errorf("failed to match any node: %w", ErrNoDirectCredentials)
}

// canSupplyCredentials reports whether a vertex can produce credentials on its own:
// either it has credentials attached directly, or it has at least one non-cyclic edge
// to a child that resolution can follow. A dependency-only vertex (added solely because
// another entry references it) has neither.
func (g *syncedDag) canSupplyCredentials(v *dag.Vertex[string]) bool {
	if _, ok := v.Attributes[attributeCredentials].(runtime.Typed); ok {
		return true
	}
	for _, edgeAttributes := range v.Edges {
		if edgeAttributes["kind"] != "cyclic-only" {
			return true
		}
	}
	return false
}

// preferMatch reports whether a candidate (supplies/identity) should replace the current
// best (bestSupplies/bestIdentity). A credential-supplying vertex always beats a
// non-supplying one; otherwise the more specific identity wins.
func preferMatch(supplies bool, identity runtime.Identity, bestSupplies bool, bestIdentity runtime.Identity) bool {
	if supplies != bestSupplies {
		return supplies
	}
	return moreSpecific(identity, bestIdentity)
}

// moreSpecific reports whether identity a is more specific than identity b.
//
// Specificity is ranked in this order:
//  1. The identity that sets more scope attributes is more specific (for example an
//     entry scoped to an explicit port or path beats a hostname-only entry). An empty
//     path is not a scope attribute: the path matcher treats it as "match any path", so
//     it is ignored when counting attributes.
//  2. For an equal attribute count, the identity whose path glob has fewer wildcard
//     tokens is more specific (for example "my-org/*" beats "*/*").
//  3. As a final, stable tie-break, the lexicographically smaller identity string is
//     treated as more specific, so selection is deterministic.
func moreSpecific(a, b runtime.Identity) bool {
	if sa, sb := scopeAttributeCount(a), scopeAttributeCount(b); sa != sb {
		return sa > sb
	}
	if aw, bw := pathWildcards(a), pathWildcards(b); aw != bw {
		return aw < bw
	}
	return a.String() < b.String()
}

// scopeAttributeCount counts the attributes that constrain matching. An empty path does
// not constrain anything (it matches every path), so it is not counted.
func scopeAttributeCount(i runtime.Identity) int {
	n := len(i)
	if v, ok := i[runtime.IdentityAttributePath]; ok && v == "" {
		n--
	}
	return n
}

// pathWildcards counts the glob wildcard tokens in the path attribute of an identity.
// Both "*" and "?" are wildcards for the glob matcher. A backslash escapes the next
// character, so "\*" and "\?" are literals and are not counted. An identity with no
// path, or a path with no wildcards, counts as 0.
func pathWildcards(i runtime.Identity) int {
	path := i[runtime.IdentityAttributePath]
	count := 0
	for j := 0; j < len(path); j++ {
		switch path[j] {
		case '\\':
			j++ // skip the escaped character
		case '*', '?':
			count++
		}
	}
	return count
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
