package constructor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
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
	mu sync.RWMutex
	// descriptors use the same key as the DAG, i.e. the component identity
	// string. Since ProcessValue is guaranteed to be called at most once for
	// each node, this is safe.
	descriptors map[string]*descriptor.Descriptor
}

func (c *descriptors) store(_ context.Context, descriptor *descriptor.Descriptor) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := descriptor.Component.ToIdentity().String()
	if _, ok := c.descriptors[id]; ok {
		return fmt.Errorf("descriptor for %s has already been processed", id)
	}
	c.descriptors[id] = descriptor
	return nil
}

func (c *descriptors) load(_ context.Context, id string) (*descriptor.Descriptor, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	desc, ok := c.descriptors[id]
	if !ok {
		return nil, fmt.Errorf("descriptor for %s not found", id)
	}
	return desc, nil
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
