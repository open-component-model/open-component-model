package input

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/constructor/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

// AddColocatedLocalBlob adds a local blob to the component version repository and defaults fields relevant
// to declare the spec.LocalRelation to the component version as well as default the resource version, media type and size.
// The resource is expected to be a local resource so the access that is created is always a local blob.
func AddColocatedLocalBlob(
	ctx context.Context,
	repo TargetRepository,
	component, version string,
	resource *spec.Resource,
	data blob.ReadOnlyBlob,
) (processed *descriptor.Resource, err error) {
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
