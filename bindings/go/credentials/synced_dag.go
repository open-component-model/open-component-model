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
// If an exact match is not found, it falls back to a wildcard search by comparing identities
// using the Identity.Match method.
// This wildcard search is the reason there can be undiscovered cycles at runtime.
func (g *syncedDag) matchAnyNode(identity runtime.Identity) (*dag.Vertex[string], error) {
	g.dagMu.RLock()
	defer g.dagMu.RUnlock()
	node := identity.String()
	if vertex, ok := g.dag.Vertices[node]; ok {
		return vertex, nil
	}
	for _, vertex := range g.dag.Vertices {
		existing := vertex.Attributes[attributeIdentity].(runtime.Identity)
		if identity.Match(existing) {
			return vertex, nil
		}
	}
	return nil, fmt.Errorf("failed to match any node: %w", ErrNoDirectCredentials)
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
		existing := vertex.Attributes[attributeIdentity].(runtime.Identity)
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
