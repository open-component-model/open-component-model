package resource

import (
	"context"

	"ocm.software/open-component-model/bindings/go/blob"
	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Repository defines the interface for storing and retrieving OCM resources
// independently of component versions from a Store Implementation
type Repository interface {
	// GetResourceCredentialConsumerIdentity resolves the identity of the given [constructor.Resource] to use for credential resolution.
	GetResourceCredentialConsumerIdentity(ctx context.Context, resource *constructor.Resource) (runtime.Identity, error)
	// UploadResource uploads a [descriptor.Resource] to the repository.
	// Returns the updated resource with repository-specific information.
	// The resource must be referenced in the component descriptor.
	// Note that UploadResource is special in that it considers both
	// - the Access from [descriptor.Resource.Access]
	// - the Target Access from the given target specification
	// It might be that during the upload, the source pointer may be updated with information gathered during upload
	// (e.g., digest, size, etc).
	UploadResource(ctx context.Context, targetAccess runtime.Typed, source *descriptor.Resource, content blob.ReadOnlyBlob, credentials map[string]string) (*descriptor.Resource, error)

	// DownloadResource downloads a [descriptor.Resource] from the repository.
	DownloadResource(ctx context.Context, res *descriptor.Resource, credentials map[string]string) (blob.ReadOnlyBlob, error)
}
