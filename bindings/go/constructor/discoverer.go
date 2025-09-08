package constructor

import (
	"context"
	"fmt"
	"log/slog"

	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

type neighborDiscoverer struct {
	constructor *DefaultConstructor
	dag         *syncdag.DirectedAcyclicGraph[string]
}

var _ syncdag.NeighborDiscoverer[string] = (*neighborDiscoverer)(nil)

func newNeighborDiscoverer(constructor *DefaultConstructor, dag *syncdag.DirectedAcyclicGraph[string]) *neighborDiscoverer {
	return &neighborDiscoverer{
		constructor: constructor,
		dag:         dag,
	}
}

func (d *neighborDiscoverer) DiscoverNeighbors(ctx context.Context, v string) (neighbors []string, err error) {
	vertex := d.dag.MustGetVertex(v)
	_, isInternal := vertex.GetAttribute(attributeComponentConstructor)
	if !isInternal {
		// This means we are on an external component node (= component is
		// not in the constructor specification).
		slog.DebugContext(ctx, "discovering external component", "component", vertex.ID)
		_, err := d.discoverExternalComponent(ctx, vertex)
		if err != nil {
			return nil, fmt.Errorf("failed to discover external component: %w", err)
		}
		return nil, nil
	}
	// This means we are on a constructor node (= component is in the
	// constructor specification).
	slog.DebugContext(ctx, "discovering internal component", "component", vertex.ID)
	neighbors, err = d.discoverInternalComponent(vertex)
	if err != nil {
		return nil, fmt.Errorf("failed to discover internal component: %w", err)
	}
	return neighbors, nil
}

// initializeDAGWithConstructor initializes the DAG with the components from the
// constructor specification.
// Due to this initialization, we do not have to fetch the constructor components
// during the discovery.
func (d *neighborDiscoverer) initializeDAGWithConstructor(constructor *constructor.ComponentConstructor) ([]string, error) {
	roots := make([]string, len(constructor.Components))

	for index, component := range constructor.Components {
		root := component.ToIdentity().String()
		if err := d.dag.AddVertex(component.ToIdentity().String(), map[string]any{
			attributeComponentConstructor: &component,
		}); err != nil {
			return nil, fmt.Errorf("failed to add root %q to dag: %w", root, err)
		}
		roots[index] = root
	}
	return roots, nil
}

// discoverInternalComponent discovers a component from the internal constructor
// specification.
func (d *neighborDiscoverer) discoverInternalComponent(vertex *syncdag.Vertex[string]) ([]string, error) {
	component := vertex.MustGetAttribute(attributeComponentConstructor).(*constructor.Component)
	neighbors := make([]string, len(component.References))
	for index, ref := range component.References {
		neighbors[index] = ref.ToComponentIdentity().String()
	}
	return neighbors, nil
}

// discoverExternalComponent discovers a component from an external repository.
// So, a component that is not part of the current constructor specification.
// TODO(fabianburth): Once we support recursive, we need to discover
//
//	their neighbors too. Are there valid cases where an external
//	component might reference a component in the constructor?
func (d *neighborDiscoverer) discoverExternalComponent(ctx context.Context, vertex *syncdag.Vertex[string]) ([]string, error) {
	identity, err := ocmruntime.ParseIdentity(vertex.ID)
	if err != nil {
		return nil, fmt.Errorf("failed parsing identity %q: %w", vertex.ID, err)
	}
	repo, err := d.constructor.opts.GetExternalRepository(ctx, identity[descriptor.IdentityAttributeName], identity[descriptor.IdentityAttributeVersion])
	if err != nil {
		return nil, fmt.Errorf("error getting external repository for component %q: %w", identity.String(), err)
	}
	// We do not need to cache here. The id of the vertex is the globally
	// unique identity of the component version. During discovery, each vertex
	// is discovered at most once - even if it is referenced by two different
	// components.
	desc, err := repo.GetComponentVersion(ctx, identity[descriptor.IdentityAttributeName], identity[descriptor.IdentityAttributeVersion])
	if err != nil {
		return nil, fmt.Errorf("error getting component version %q from repository: %w", identity.String(), err)
	}
	vertex.Attributes.Store(attributeComponentDescriptor, desc)

	// TODO(fabianburth): once we support recursive, we need to discover the
	//  neighbors here (https://github.com/open-component-model/ocm-project/issues/666)
	return nil, nil
}
