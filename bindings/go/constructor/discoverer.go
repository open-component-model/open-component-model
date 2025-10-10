package constructor

import (
	"context"
	"errors"
	"fmt"

	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

type resolverAndDiscoverer struct {
	componentConstructor                *constructor.ComponentConstructor
	externalComponentRepositoryProvider ExternalComponentRepositoryProvider
}

var (
	_ syncdag.Resolver[string, *ConstructorOrExternalComponent]   = (*resolverAndDiscoverer)(nil)
	_ syncdag.Discoverer[string, *ConstructorOrExternalComponent] = (*resolverAndDiscoverer)(nil)
)

func (d *resolverAndDiscoverer) Resolve(ctx context.Context, id string) (*ConstructorOrExternalComponent, error) {
	constructorComponent, err := d.resolveConstructorComponent(ctx, id)
	if err == nil {
		return &ConstructorOrExternalComponent{
			ConstructorComponent: constructorComponent,
		}, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("error resolving constructor component %q: %w", id, err)
	}

	// Not found in constructor, try external repository.
	externalComponent, err := d.resolveExternalComponent(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("error resolving external component %q: %w", id, err)
	}
	return &ConstructorOrExternalComponent{
		ExternalComponent: externalComponent,
	}, nil
}

func (d *resolverAndDiscoverer) Discover(ctx context.Context, component *ConstructorOrExternalComponent) ([]string, error) {
	switch {
	case component.ConstructorComponent != nil:
		children := make([]string, len(component.ConstructorComponent.References))
		for index, ref := range component.ConstructorComponent.References {
			children[index] = ref.ToComponentIdentity().String()
		}
		return children, nil
	case component.ExternalComponent != nil:
		children := make([]string, len(component.ExternalComponent.Component.References))
		for index, ref := range component.ExternalComponent.Component.References {
			children[index] = ref.ToComponentIdentity().String()
		}
		return children, nil
	}
	return nil, fmt.Errorf("constructor or external component must have either a constructor component or an external component")
}

func (d *resolverAndDiscoverer) resolveConstructorComponent(_ context.Context, id string) (*constructor.Component, error) {
	for _, component := range d.componentConstructor.Components {
		identity := component.ToIdentity().String()
		if identity == id {
			return &component, nil
		}
	}
	return nil, fmt.Errorf("component %s not found in constructor: %w", id, repository.ErrNotFound)
}

func (d *resolverAndDiscoverer) resolveExternalComponent(ctx context.Context, id string) (*descriptor.Descriptor, error) {
	identity, err := ocmruntime.ParseIdentity(id)
	if err != nil {
		return nil, fmt.Errorf("failed parsing identity %q: %w", id, err)
	}
	repo, err := d.externalComponentRepositoryProvider.GetExternalRepository(ctx, identity[descriptor.IdentityAttributeName], identity[descriptor.IdentityAttributeVersion])
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
	return desc, nil
}
