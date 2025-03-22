package oci

import (
	"context"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptorRuntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorRuntime "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

// OCMComponentVersionRepository is a repository that can store and retrieve Component Descriptors based on a
// component version, as well as store correlated data (local resources) that are stored next to the component version.
type OCMComponentVersionRepository interface {
	// AddComponentVersion adds a new component version to the repository.
	// If the component under that version exists, it is expected that once this call returns successfully,
	// the component version is available for retrieval with the new descriptor.
	AddComponentVersion(ctx context.Context, descriptor *descriptorRuntime.Descriptor) error
	// GetComponentVersion retrieves a component version from the repository. It will contain the descriptor
	// from the last AddComponentVersion call made to that component and version.
	GetComponentVersion(ctx context.Context, component, version string) (*descriptorRuntime.Descriptor, error)
	// AddLocalResource adds a local resource to the repository. compared to AddComponentVersion,
	// the resource is not an identifier on its own, so storing a resource for a component version that does not
	// yet exist can be done, but may not be persisted beyond a garbage collection that removes unreferenced resources.
	// note that the identity needs to match an identity in the component descriptor for local resources.
	AddLocalResource(ctx context.Context, component, version string, res *descriptorRuntime.Resource, content blob.ReadOnlyBlob) (newRes *descriptorRuntime.Resource, err error)

	// GetLocalResource retrieves a local resource from the repository. The identity is used to determine which resource
	// to retrieve. If the identity does not match any resource in the descriptor, there is no guarantee that the resource
	// can be returned.
	GetLocalResource(ctx context.Context, component, version string, identity map[string]string) (descriptorRuntime.LocalBlob, error)
}

// OCMResourceRepository is a repository that can store and retrieve resources independently of component versions.
// It can be used to store resources that are not directly associated with a component version, but also to transfer
// resources between repositories that may not be stored alongside the component version itself
type OCMResourceRepository interface {
	UploadResource(ctx context.Context, res *descriptorRuntime.Resource, content blob.ReadOnlyBlob) (newRes *descriptorRuntime.Resource, err error)
	DownloadResource(ctx context.Context, res *descriptorRuntime.Resource) (content blob.ReadOnlyBlob, err error)
}
