package constructor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"ocm.software/open-component-model/bindings/go/constructor/internal/log"
	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/algorithms"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/repository"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

type resolverAndDiscoverer struct {
	componentConstructor                *constructor.ComponentConstructor
	externalComponentRepositoryProvider ExternalComponentRepositoryProvider
	resolveExternalLocalBlobs           bool
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
			// A reference without a digest is minted with the default.
			if ref.Digest != nil {
				algorithms.LogNonDefaultReferenceDigest(ctx, log.Base(), slog.LevelWarn, constructor.ConvertToDescriptorReference(&ref).Digest, "component", component.ConstructorComponent.ToIdentity().String(), "reference", ref.ToIdentity().String())
			}
		}
		return children, nil
	case component.ExternalComponent != nil:
		children := make([]string, len(component.ExternalComponent.Component.References))
		for index, ref := range component.ExternalComponent.Component.References {
			children[index] = ref.ToComponentIdentity().String()
			// Published by someone else: the user cannot change it, and it does
			// not affect the digest of the component version being built.
			algorithms.LogNonDefaultReferenceDigest(ctx, log.Base(), slog.LevelDebug, ref.Digest,
				"component", component.ExternalComponent.Component.ToIdentity().String(), "reference", ref.ToIdentity().String())
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

// DescriptorWithLocalBlobs is an external component version together with what is
// needed to copy its local blobs.
type DescriptorWithLocalBlobs struct {
	*descriptor.Descriptor
	// Source is the repository the component version was resolved from. It is set
	// only if the local blobs are copied.
	Source repository.ComponentVersionRepository
	// Local holds the indexes of the resources stored as local blobs.
	Local []int
}

func (d *resolverAndDiscoverer) resolveExternalComponent(ctx context.Context, id string) (*DescriptorWithLocalBlobs, error) {
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

	descriptorWithLocalBlobs := &DescriptorWithLocalBlobs{Descriptor: desc}
	if d.resolveExternalLocalBlobs {
		// The blobs are fetched when the component is processed, not here: a blob may be
		// read only while the context it was fetched with lasts, and discovery's ends
		// before processing starts.
		descriptorWithLocalBlobs.Source = repo
		for idx, resource := range desc.Component.Resources {
			if err := v2.Scheme.Convert(resource.Access, &v2.LocalBlob{}); err == nil {
				descriptorWithLocalBlobs.Local = append(descriptorWithLocalBlobs.Local, idx)
			}
		}
	}

	return descriptorWithLocalBlobs, nil
}
