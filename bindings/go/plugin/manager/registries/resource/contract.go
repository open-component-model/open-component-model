package resource

import (
	"context"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type RepositoryProvider interface {
	// GetResourceRepositoryCredentialConsumerIdentity retrieves the consumer identity for a given repository specification.
	GetResourceRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error)
	// GetResourceRepository retrieves a resource repository with the given specification and credentials.
	GetResourceRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (Repository, error)
}

// Repository defines the interface for storing and retrieving OCM resources
// independently of component versions from a Store Implementation
type Repository interface {
	// GetResourceCredentialConsumerIdentity resolves the identity of the given [descriptor.Resource] to use for credential resolution.
	GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error)
	// DownloadResource downloads a [descriptor.Resource] from the repository.
	DownloadResource(ctx context.Context, res *descriptor.Resource, credentials map[string]string) (blob.ReadOnlyBlob, error)
}
