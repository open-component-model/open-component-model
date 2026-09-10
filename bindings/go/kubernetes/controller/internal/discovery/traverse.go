package discovery

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/dag"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// resolverAndDiscoverer resolves each component version through a configured
// repository resolver and discovers its transitive references. It implements
// both syncdag.Resolver and syncdag.Discoverer over string keys of the form
// produced by runtime.Identity.String (name=...,version=...).
type resolverAndDiscoverer struct {
	resolver resolvers.ComponentVersionRepositoryResolver
}

// Resolve selects the repository for the given component identity via the
// configured resolver and fetches its descriptor. Repository-selection and
// fetch failures are wrapped with the component identity.
func (rd *resolverAndDiscoverer) Resolve(ctx context.Context, key string) (*descriptor.Descriptor, error) {
	id, err := runtime.ParseIdentity(key)
	if err != nil {
		return nil, fmt.Errorf("failed to parse component key %q: %w", key, err)
	}
	name, version := id[descriptor.IdentityAttributeName], id[descriptor.IdentityAttributeVersion]

	repo, err := rd.resolver.GetComponentVersionRepositoryForComponent(ctx, name, version)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve repository for component %s: %w", key, err)
	}

	desc, err := repo.GetComponentVersion(ctx, name, version)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve component %s: %w", key, err)
	}
	if desc == nil {
		return nil, fmt.Errorf("failed to resolve component %s: resolved descriptor is nil", key)
	}

	return desc, nil
}

// Discover returns the component identities of every reference of parent.
// Duplicate reference entries to the same target are naturally deduplicated by
// the DAG (syncdag resolves each vertex once, dag.AddEdge accepts repeated
// edges); the descriptor reference entries themselves remain untouched.
func (rd *resolverAndDiscoverer) Discover(_ context.Context, parent *descriptor.Descriptor) ([]string, error) {
	neighbors := make([]string, 0, len(parent.Component.References))
	for i := range parent.Component.References {
		neighbors = append(neighbors, parent.Component.References[i].ToComponentIdentity().String())
	}
	return neighbors, nil
}

// Traverse resolves the complete transitive component graph reachable from
// root through component references. The repository for every component
// identity is chosen by resolver (following CLI resolver precedence:
// configured path matchers or deprecated fallback resolvers, with an optional
// high-priority root pattern and root-repository catch-all). Every referenced
// component version is resolved exactly once; duplicate reference entries to
// the same target are deduplicated while the descriptor reference entries
// themselves remain untouched.
//
// The traversal is fail-fast: the first resolution failure cancels the
// remaining traversal and is returned wrapped with the failing component
// identity. The graph is only consumed after the entire traversal succeeded;
// on error no partial graph is returned.
//
// Cycle detection is intentionally not implemented here; it is handled by the
// shared DAG backlog issue (open-component-model/ocm-project#705).
func Traverse(ctx context.Context, root ComponentKey, resolver resolvers.ComponentVersionRepositoryResolver) (*Graph, error) {
	if resolver == nil {
		return nil, fmt.Errorf("component version repository resolver must not be nil")
	}

	rootKey := runtime.Identity{
		descriptor.IdentityAttributeName:    root.Name,
		descriptor.IdentityAttributeVersion: root.Version,
	}.String()

	rd := &resolverAndDiscoverer{resolver: resolver}
	discoverer := syncdag.NewGraphDiscoverer(&syncdag.GraphDiscovererOptions[string, *descriptor.Descriptor]{
		Roots:      []string{rootKey},
		Resolver:   syncdag.ResolverFunc[string, *descriptor.Descriptor](rd.Resolve),
		Discoverer: syncdag.DiscovererFunc[string, *descriptor.Descriptor](rd.Discover),
	})

	if err := discoverer.Discover(ctx); err != nil {
		return nil, err
	}

	// Consume the discovered vertices only after the entire traversal succeeded.
	graph := &Graph{Root: root}
	if err := discoverer.Graph().WithReadLock(func(d *dag.DirectedAcyclicGraph[string]) error {
		descriptors := make([]*descriptor.Descriptor, 0, len(d.Vertices))
		for key, vertex := range d.Vertices {
			desc, ok := vertex.Attributes[syncdag.AttributeValue].(*descriptor.Descriptor)
			if !ok || desc == nil {
				return fmt.Errorf("discovered vertex %v has no resolved descriptor", key)
			}
			descriptors = append(descriptors, desc)
		}
		graph.Descriptors = descriptors
		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to collect discovered graph: %w", err)
	}

	return graph, nil
}
