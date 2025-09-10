package constructor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

type vertexProcessor struct {
	constructor *DefaultConstructor
	dag         *syncdag.DirectedAcyclicGraph[string]

	descriptorsMu sync.Mutex
	descriptors   []*descriptor.Descriptor
}

var _ syncdag.VertexProcessor[string] = (*vertexProcessor)(nil)

func newVertexProcessor(constructor *DefaultConstructor, dag *syncdag.DirectedAcyclicGraph[string]) *vertexProcessor {
	descriptors := make([]*descriptor.Descriptor, 0, dag.LengthVertices())
	return &vertexProcessor{
		constructor: constructor,
		dag:         dag,
		descriptors: descriptors,
	}
}

func (p *vertexProcessor) ProcessVertex(ctx context.Context, v string) error {
	vertex := p.dag.MustGetVertex(v)
	_, isInternal := vertex.GetAttribute(attributeComponentConstructor)

	var (
		err  error
		desc *descriptor.Descriptor
	)
	if !isInternal {
		// This means we are on an external component node (= component is
		// not in the constructor specification).
		slog.DebugContext(ctx, "processing external component", "component", vertex.ID)
		// TODO(fabianburth): once we support recursive, we need to perform
		//  the transfer of the component here.
		// desc, err = processExternalComponent(vertex)
		return nil
	} else {
		// This means we are on a constructor node (= component is in the
		// constructor specification).
		slog.DebugContext(ctx, "processing internal component", "component", vertex.ID)
		desc, err = p.processInternalComponent(ctx, vertex)
		if err != nil {
			return fmt.Errorf("failed to process internal component: %w", err)
		}
	}

	p.descriptorsMu.Lock()
	defer p.descriptorsMu.Unlock()
	p.descriptors = append(p.descriptors, desc)
	slog.DebugContext(ctx, "component constructed successfully")

	return nil
}

// processInternalComponent processes a component from the internal constructor
// specification.
func (p *vertexProcessor) processInternalComponent(ctx context.Context, vertex *syncdag.Vertex[string]) (*descriptor.Descriptor, error) {
	component := vertex.MustGetAttribute(attributeComponentConstructor).(*constructor.Component)
	referencedComponents := make(map[string]*descriptor.Descriptor, len(component.References))
	// Collect the descriptors of all referenced components to calculate their
	// digest for the component reference.
	for _, ref := range component.References {
		identity := ocmruntime.Identity{
			descriptor.IdentityAttributeName:    ref.Component,
			descriptor.IdentityAttributeVersion: ref.Version,
		}
		refVertex, exists := p.dag.GetVertex(identity.String())
		if !exists {
			return nil, fmt.Errorf("missing dependency %q for component %q", identity.String(), component.ToIdentity())
		}
		// Since ProcessTopology is called with reverse, referenced components
		// must have been processed already. Therefore, we expect the descriptor
		// to be available.
		untypedRefDescriptor, ok := refVertex.GetAttribute(attributeComponentDescriptor)
		if !ok {
			return nil, fmt.Errorf("missing descriptor for dependency %q of component %q", identity.String(), component.ToIdentity())
		}
		refDescriptor := untypedRefDescriptor.(*descriptor.Descriptor)
		referencedComponents[ref.ToIdentity().String()] = refDescriptor
	}
	desc, err := p.constructor.constructComponent(ctx, component, referencedComponents)
	if err != nil {
		return nil, fmt.Errorf("error constructing component %q: %w", component.ToIdentity(), err)
	}
	vertex.Attributes.Store(attributeComponentDescriptor, desc)
	return desc, nil
}
