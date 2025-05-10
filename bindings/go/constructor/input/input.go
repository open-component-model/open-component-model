package input

import (
	"context"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/constructor/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// TargetRepository defines the interface for storing additional local resources created when processing inputs
// with the constructor
type TargetRepository interface {
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
	// to upload resources via [TargetRepository.AddLocalResource].
	Target TargetRepository
}

// ResourceInputMethod is the interface for processing a resource with an input method decaled as per
// [spec.Resource.Input].
//
// The method will get called with the raw specification for the resource and is expected
// to return a runtime resource specification with [descriptor.Resource.Access] set to the
// now resulting access type.
//
// For Input Methods wishing to colocate their uploaded resources with the component version
// they are part of, the targeted [TargetRepository] is passed in via the Options struct,
// and can be used to upload the resource into the OCM component version repository.
type ResourceInputMethod interface {
	ProcessResource(ctx context.Context, resource *spec.Resource, opts Options) (processed *descriptor.Resource, err error)
}
