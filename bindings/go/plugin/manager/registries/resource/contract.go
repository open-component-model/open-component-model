package resource

import (
	"context"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Repository defines the interface for storing and retrieving OCM resources
// independently of component versions from a Store Implementation
type Repository interface {
	// GetResourceCredentialConsumerIdentity resolves the identity of the given [descriptor.Resource] to use for credential resolution.
	GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error)
	// DownloadResource downloads and verifies the integrity of a [descriptor.Resource] from the repository.
	DownloadResource(ctx context.Context, res *descriptor.Resource, credentials map[string]string) (blob.ReadOnlyBlob, error)
}

// The BuiltinResourceRepository has the primary purpose to allow plugin
// registries to register internal plugins without requiring callers to
// explicitly provide a scheme with their supported types.
// A scheme is mapping types to their go types. As the go types of external
// plugins are not compiled in, they cannot have a scheme and therefore, cannot
// implement this interface.
type BuiltinResourceRepository interface {
	Repository
	GetResourceRepositoryScheme() *runtime.Scheme
}
