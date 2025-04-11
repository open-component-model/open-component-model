package credentials

import (
	"fmt"
	"sync"

	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/pocm/bindings/golang/dag"
)

const attributeIdentity = "attributes.ocm.software/identity"

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
	return
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
	return nil, fmt.Errorf("failed to resolve credentials for node %v: %w", node, ErrNoDirectCredentials)
}

// addIdentityToGraph ensures that a given identity is represented as a vertex in the graph.
// It also establishes edges between the new node and any existing nodes that match with each other.
func (g *syncedDag) addIdentityToGraph(identity runtime.Identity) error {
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
			if err := g.dag.AddEdge(vertex.ID, node); err != nil {
				return err
			}
		}
		if existing.Match(identity) {
			if err := g.dag.AddEdge(node, vertex.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
