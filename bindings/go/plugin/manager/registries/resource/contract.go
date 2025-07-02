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
	// DownloadResource downloads a [descriptor.Resource] from the repository.
	DownloadResource(ctx context.Context, res *descriptor.Resource, credentials map[string]string) (blob.ReadOnlyBlob, error)
}
