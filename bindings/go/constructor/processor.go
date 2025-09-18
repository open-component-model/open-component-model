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

// vertexProcessor is responsible for processing discovered component in the DAG.
// Hereby, processing means:
// - constructing components that are part of the constructor specification
// - uploading components to the target repository
type vertexProcessor struct {
	constructor *DefaultConstructor
	dag         *syncdag.DirectedAcyclicGraph[string]
}

var _ syncdag.VertexProcessor[string] = (*vertexProcessor)(nil)

func newVertexProcessor(constructor *DefaultConstructor, dag *syncdag.DirectedAcyclicGraph[string]) *vertexProcessor {
	return &vertexProcessor{
		constructor: constructor,
		dag:         dag,
	}
}

// ProcessVertex processes the component represented by the given vertex.
func (p *vertexProcessor) ProcessVertex(ctx context.Context, v string) error {
	vertex := p.dag.MustGetVertex(v)
	_, isInternal := vertex.GetAttribute(attributeComponentConstructor)

	if !isInternal {
		// This means we are on an external component node (= component is
		// not in the constructor specification).
		slog.DebugContext(ctx, "processing external component", "component", vertex.ID)
		// TODO(fabianburth): once we support recursive, we need to perform
		//  the transfer of the component here (https://github.com/open-component-model/ocm-project/issues/666).
		// desc, err = processExternalComponent(vertex)
		return nil
	} else {
		// This means we are on a constructor node (= component is in the
		// constructor specification).
		slog.DebugContext(ctx, "processing internal component", "component", vertex.ID)
		if err := p.processInternalComponent(ctx, vertex); err != nil {
			return fmt.Errorf("failed to process internal component: %w", err)
		}
	}

	slog.DebugContext(ctx, "component constructed successfully")

	return nil
}

// processInternalComponent processes a component from the internal constructor
// specification.
func (p *vertexProcessor) processInternalComponent(ctx context.Context, vertex *syncdag.Vertex[string]) error {
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
			return fmt.Errorf("missing dependency %q for component %q", identity.String(), component.ToIdentity())
		}
		// Since ProcessTopology is called with reverse, referenced components
		// must have been processed already. Therefore, we expect the descriptor
		// to be available.
		refDescriptor := refVertex.MustGetAttribute(attributeComponentDescriptor).(*descriptor.Descriptor)
		referencedComponents[ref.ToIdentity().String()] = refDescriptor
	}
	if p.constructor.opts.OnStartComponentConstruct != nil {
		if err := p.constructor.opts.OnStartComponentConstruct(ctx, component); err != nil {
			return fmt.Errorf("error starting component construction for %q: %w", component.ToIdentity(), err)
		}
	}
	desc, err := p.constructor.constructComponent(ctx, component, referencedComponents)
	if p.constructor.opts.OnEndComponentConstruct != nil {
		if err := p.constructor.opts.OnEndComponentConstruct(ctx, desc, err); err != nil {
			return fmt.Errorf("error ending component construction for %q: %w", component.ToIdentity(), err)
		}
	}
	if err != nil {
		return fmt.Errorf("error constructing component %q: %w", component.ToIdentity(), err)
	}
	vertex.Attributes.Store(attributeComponentDescriptor, desc)
	return nil
}
