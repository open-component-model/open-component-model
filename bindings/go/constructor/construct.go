package constructor

import (
	"context"
	"errors"
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/constructor/input/registry"
	"ocm.software/open-component-model/bindings/go/constructor/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ComponentVersionRepository defines the interface for storing and retrieving OCM component versions
// and their associated resources in a Store.
type ComponentVersionRepository interface {
	// AddComponentVersion adds a new component version to the repository.
	// If a component version already exists, it will be updated with the new descriptor.
	// The descriptor internally will be serialized via the runtime package.
	// The descriptor MUST have its target Name and Version already set as they are used to identify the target
	// Location in the Store.
	AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error
	// AddLocalResource adds a local resource to the repository.
	// The resource must be referenced in the component descriptor.
	// Resources for non-existent component versions may be stored but may be removed during garbage collection.
	// The Resource given is identified later on by its own Identity and a collection of a set of reserved identity values
	// that can have a special meaning.
	AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (newRes *descriptor.Resource, err error)
}

// ResourceRepository defines the interface for storing and retrieving OCM resources
// independently of component versions from a Store Implementation
type ResourceRepository interface {
	// UploadResource uploads a resource to the repository.
	// Returns the updated resource with repository-specific information.
	// The resource must be referenced in the component descriptor.
	// Note that UploadResource is special in that it considers both
	// - the Source Access from descriptor.Resource
	// - the Target Access from the given target specification
	UploadResource(ctx context.Context, targetAccess runtime.Typed, source *descriptor.Resource, content blob.ReadOnlyBlob) (err error)
	// DownloadResource downloads a resource from the repository.
	DownloadResource(ctx context.Context, res *descriptor.Resource) (content blob.ReadOnlyBlob, err error)
}

type Options struct {
	Target              ComponentVersionRepository
	InputMethodRegistry *registry.Registry
}

func Construct(ctx context.Context, constructor *spec.ComponentConstructor, opts Options) ([]*descriptor.Descriptor, error) {
	if err := spec.Validate(constructor); err != nil {
		return nil, err
	}
	if opts.InputMethodRegistry == nil {
		opts.InputMethodRegistry = registry.Default
	}
	descriptors := make([]*descriptor.Descriptor, 0)
	for _, component := range constructor.Components {
		desc, err := construct(ctx, component, opts)
		if err != nil {
			return nil, err
		}
		descriptors = append(descriptors, desc)
	}
	return descriptors, nil
}

func construct(ctx context.Context, component spec.Component, opts Options) (*descriptor.Descriptor, error) {
	if err := Validate(component); err != nil {
		return nil, err
	}

	desc := descriptor.Descriptor{
		Meta: descriptor.Meta{
			Version: "v2",
		},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    component.Name,
					Version: component.Version,
					Labels:  spec.ConvertFromLabels(component.Labels),
				},
			},
			Provider: map[string]string{
				"name": component.Provider.Name,
			},
			Resources:          make([]descriptor.Resource, 0, len(component.Resources)),
			Sources:            make([]descriptor.Source, 0),
			References:         make([]descriptor.Reference, 0),
			RepositoryContexts: make([]runtime.Typed, 0),
		},
	}

	for i, resource := range component.Resources {
		var data blob.ReadOnlyBlob
		var access runtime.Typed
		if resource.HasInput() {
			method, found := opts.InputMethodRegistry.GetFor(resource.Input)
			if !found {
				return nil, fmt.Errorf("no input method found for input specification of type %q", resource.Input.GetType())
			}
			var err error
			if data, err = method.ProcessResource(ctx, &resource); err != nil {
				return nil, fmt.Errorf("error getting blob from input method: %w", err)
			}
			localBlob := &v2.LocalBlob{}

			if mediaTypeAware, ok := data.(blob.MediaTypeAware); ok {
				localBlob.MediaType, _ = mediaTypeAware.MediaType()
			}
			access = localBlob
		}
		descResource := spec.ConvertToRuntimeResource(resource)

		// if the resource doesn't have any information about its relation to the component
		// default to a local resource.
		if descResource.Relation == "" {
			descResource.Relation = descriptor.LocalRelation
		}

		// if the resource doesn't have any information about its version,
		// default to the component version.
		if descResource.Version == "" {
			descResource.Version = component.Version
		}

		// if the data is size aware, set the size in the resource
		if sizeAware, ok := data.(blob.SizeAware); ok {
			descResource.Size = sizeAware.Size()
		}

		if !resource.HasAccess() {
			descResource.Access = access
			uploaded, err := opts.Target.AddLocalResource(ctx, component.Name, component.Version, &descResource, data)
			if err != nil {
				return nil, fmt.Errorf("error adding local resource at index %d based on input type %v to target: %w", i, resource.Input.GetType(), err)
			}
			descResource = *uploaded
		}

		desc.Component.Resources = append(desc.Component.Resources, descResource)
	}

	if err := opts.Target.AddComponentVersion(ctx, &desc); err != nil {
		return nil, fmt.Errorf("error adding component version to target: %w", err)
	}

	return &desc, nil
}

func Validate(component spec.Component) error {
	errs := make([]error, 0, len(component.Resources)+len(component.Sources))
	for _, resource := range component.Resources {
		if err := resource.Validate(); err != nil {
			errs = append(errs, err)
		}
	}
	for _, source := range component.Sources {
		if err := source.Validate(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
