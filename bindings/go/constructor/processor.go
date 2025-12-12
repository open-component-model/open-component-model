package constructor

import (
	"context"
	"fmt"
	"log/slog"

	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/dag"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// processor is responsible for processing discovered component in the DAG.
// Hereby, processing means:
// - constructing components that are part of the constructor specification
// - uploading components to the target repository
type processor struct {
	constructor          *DefaultConstructor
	processedDescriptors descriptors
}

type descriptors struct {
	// graph is the DAG containing the components to be processed.
	// It needs to have the full topology already discovered.
	// The processor will add the constructed/uploaded component descriptors as
	// attributes to the corresponding vertices in the DAG. The key used for
	// storing the descriptor is AttributeDescriptor.
	// The original data of the graph is of type *ConstructorOrExternalComponent and will stay unchanged.
	graph *syncdag.SyncedDirectedAcyclicGraph[string]
}

func (c *descriptors) load(_ context.Context, id string) (*descriptor.Descriptor, error) {
	var vert *dag.Vertex[string]
	if err := c.graph.WithReadLock(func(d *dag.DirectedAcyclicGraph[string]) error {
		if v, ok := d.Vertices[id]; !ok {
			return fmt.Errorf("descriptor for %s not found in DAG", id)
		} else {
			vert = v
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to access DAG: %w", err)
	}

	val, ok := vert.Attributes[AttributeDescriptor]
	if !ok {
		return nil, fmt.Errorf("no attributes found for vertex %s", id)
	}
	desc, ok := val.(*descriptor.Descriptor)
	if !ok {
		return nil, fmt.Errorf("attribute value for vertex %s is not of type *descriptor.Descriptor", id)
	}

	return desc, nil
}

func (c *descriptors) store(_ context.Context, descriptor *descriptor.Descriptor) error {
	id := descriptor.Component.ToIdentity().String()

	err := c.graph.WithWriteLock(func(d *dag.DirectedAcyclicGraph[string]) error {
		var vert *dag.Vertex[string]
		if v, ok := d.Vertices[id]; !ok {
			return fmt.Errorf("descriptor for %s not found in DAG", id)
		} else {
			vert = v
		}
		if _, ok := vert.Attributes[AttributeDescriptor]; ok {
			return fmt.Errorf("descriptor for %s has already been processed", id)
		}

		// add descriptor as attribute value
		vert.Attributes[AttributeDescriptor] = descriptor

		return nil
	})

	return err
}

var _ syncdag.Processor[*ConstructorOrExternalComponent] = (*processor)(nil)

func (p *processor) ProcessValue(ctx context.Context, component *ConstructorOrExternalComponent) error {
	switch {
	case component.ConstructorComponent != nil:
		if err := p.processConstructorComponent(ctx, component.ConstructorComponent); err != nil {
			return fmt.Errorf("failed processing constructor component: %w", err)
		}
	case component.ExternalComponent != nil:
		if err := p.processExternalComponent(ctx, component.ExternalComponent); err != nil {
			return fmt.Errorf("failed processing external component: %w", err)
		}
	default:
		return fmt.Errorf("expected node value of type %T to have either a constructor component or an external component", component)
	}
	return nil
}

func (p *processor) processConstructorComponent(ctx context.Context, component *constructor.Component) error {
	referencedComponents := make(map[string]*descriptor.Descriptor, len(component.References))
	// Collect the descriptors of all referenced components to calculate their
	// digest for the component reference.
	for _, ref := range component.References {
		id := ref.ToComponentIdentity().String()
		// Since ProcessTopology is called with reverse, referenced components
		// must have been processed already. Therefore, we expect the descriptor
		// to be available.
		refDescriptor, err := p.processedDescriptors.load(ctx, id)
		if err != nil {
			return fmt.Errorf("missing dependency %s for component %s", id, component.ToIdentity().String())
		}
		// We use the `ToIdentity` here, because a component may have multiple
		// references to the same component (so, multiple references may have
		// the same component name and version, but different names and/or extra
		// identities).
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
	if err := p.processedDescriptors.store(ctx, desc); err != nil {
		return fmt.Errorf("failed to store processed descriptor: %w", err)
	}
	return nil
}

func (p *processor) processExternalComponent(ctx context.Context, descriptor *descriptor.Descriptor) error {
	if p.constructor.opts.ExternalComponentVersionCopyPolicy == ExternalComponentVersionCopyPolicySkip {
		slog.DebugContext(ctx, "external component was skipped")

		if err := p.processedDescriptors.store(ctx, descriptor); err != nil {
			return fmt.Errorf("failed to store processed descriptor: %w", err)
		}
		return nil
	}

	constructorComponent := constructor.ConvertFromDescriptorComponent(&descriptor.Component)
	repo, err := p.constructor.opts.GetTargetRepository(ctx, constructorComponent)
	if err != nil {
		return fmt.Errorf("error getting target repository for component %q: %w", descriptor.Component.ToIdentity(), err)
	}
	if err := repo.AddComponentVersion(ctx, descriptor); err != nil {
		return fmt.Errorf("error adding component version to target: %w", err)
	}
	slog.DebugContext(ctx, "external component added to target repository")

	if err := p.processedDescriptors.store(ctx, descriptor); err != nil {
		return fmt.Errorf("failed to store processed descriptor: %w", err)
	}

	return nil
}
