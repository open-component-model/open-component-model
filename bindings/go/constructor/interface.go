package constructor

import (
	"context"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/constructor/input"
	"ocm.software/open-component-model/bindings/go/constructor/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Repository defines the interface for a target repository that can store component versions but also
// act as a target for the constructor to add resources to.
type Repository interface {
	input.TargetRepository
	// AddComponentVersion adds a new component version to the repository.
	// If a component version already exists, it will be updated with the new descriptor.
	// The descriptor internally will be serialized via the runtime package.
	// The descriptor MUST have its target Name and Version already set as they are used to identify the target
	// Location in the Store.
	AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error
}

type ResourceInputMethodProvider interface {
	// GetResourceInputMethod returns the input method for the given resource input specification.
	// If no input method is found, it returns false.
	GetResourceInputMethod(ctx context.Context, inputSpecification runtime.Typed) (input.ResourceInputMethod, error)
}

type ResourceInputMethodProviderFn func(ctx context.Context, inputSpecification runtime.Typed) (input.ResourceInputMethod, error)

func (fn ResourceInputMethodProviderFn) GetResourceInputMethod(ctx context.Context, inputSpecification runtime.Typed) (input.ResourceInputMethod, error) {
	return fn(ctx, inputSpecification)
}

type ResourceRepositoryProvider interface {
	// GetResourceRepository returns a ResourceRepository for the given resource.
	GetResourceRepository(ctx context.Context, resource *descriptor.Resource) (ResourceRepository, error)
}

type ResourceRepositoryProviderFn func(ctx context.Context, resource *descriptor.Resource) (ResourceRepository, error)

func (fn ResourceRepositoryProviderFn) GetResourceRepository(ctx context.Context, resource *descriptor.Resource) (ResourceRepository, error) {
	return fn(ctx, resource)
}

type ResourceRepository interface {
	// DownloadResource downloads a resource from the repository.
	// THe resource MUST contain a valid v1.OCIImage specification that exists in the Store.
	// Otherwise, the download will fail.
	//
	// The blob.ReadOnlyBlob returned will always be an OCI Layout, readable by oci.NewFromTar.
	// For more information on the download procedure, see NewOCILayoutWriter.
	DownloadResource(ctx context.Context, res *descriptor.Resource) (content blob.ReadOnlyBlob, err error)
}

type Options struct {
	Target         Repository
	ProcessByValue func(*spec.Resource) bool

	InputMethodProvider        ResourceInputMethodProvider
	ResourceRepositoryProvider ResourceRepositoryProvider
}
