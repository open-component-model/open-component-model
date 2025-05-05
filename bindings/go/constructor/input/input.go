package input

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/constructor/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
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

type Options struct {
	// Component is the name of the component version that the resources are being added to.
	Component string
	// Version is the version of the component version that the resources are being added to.
	Version string

	// Target is the targeted component version repository of the component version after all
	// inputs have been processed. It can be used by ResourceInputMethod implementations
	// to upload resources via [ComponentVersionRepository.AddLocalResource].
	Target ComponentVersionRepository
}

// ResourceInputMethod is the interface for processing a resource with an input method decaled as per
// [spec.Resource.Input].
//
// The method will get called with the raw specification for the resource and is expected
// to return a runtime resource specification with [descriptor.Resource.Access] set to the
// now resulting access type.
//
// For Input Methods wishing to colocate their uploaded resources with the component version
// they are part of, the targeted [ComponentVersionRepository] is passed in via the Options struct,
// and can be used to upload the resource into the OCM component version repository.
type ResourceInputMethod interface {
	ProcessResource(ctx context.Context, resource *spec.Resource, opts Options) (processed *descriptor.Resource, err error)
}

// AddColocatedLocalBlob adds a local blob to the component version repository and defaults fields relevant
// to declare the spec.LocalRelation to the component version as well as default the resource version, media type and size.
// The resource is expected to be a local resource so the access that is created is always a local blob.
func AddColocatedLocalBlob(ctx context.Context, repo ComponentVersionRepository, component, version string, resource *spec.Resource, data blob.ReadOnlyBlob) (processed *descriptor.Resource, err error) {
	localBlob := &v2.LocalBlob{}
	if _, err := v2.Scheme.DefaultType(localBlob); err != nil {
		return nil, fmt.Errorf("error getting default type for local blob: %w", err)
	}

	if mediaTypeAware, ok := data.(blob.MediaTypeAware); ok {
		localBlob.MediaType, _ = mediaTypeAware.MediaType()
	}

	// if the resource doesn't have any information about its relation to the component
	// default to a local resource.
	if resource.Relation == "" {
		resource.Relation = spec.LocalRelation
	}

	// if the resource doesn't have any information about its version,
	// default to the component version.
	if resource.Version == "" {
		resource.Version = version
	}

	descResource := spec.ConvertToRuntimeResource(*resource)

	// if the data is size aware, set the size in the resource
	if sizeAware, ok := data.(blob.SizeAware); ok {
		descResource.Size = sizeAware.Size()
	}

	descResource.Access = localBlob
	uploaded, err := repo.AddLocalResource(ctx, component, version, &descResource, data)
	if err != nil {
		return nil, fmt.Errorf("error adding local resource %q based on input type %q as local resource to component %q : %w", resource.ToIdentity(), resource.Input.GetType(), component, err)
	}

	return uploaded, nil
}
