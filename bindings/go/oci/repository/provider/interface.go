package provider

import (
	"context"

	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ComponentVersionRepositoryProvider is an interface that provides methods to retrieve component version repositories.
// It includes methods to get the credential consumer identity and to retrieve the repository itself based on a given specification.
// The provider can handle different types of repository specifications, such as OCI and CTF repositories.
type ComponentVersionRepositoryProvider interface {
	// GetComponentVersionRepositoryCredentialConsumerIdentity retrieves the consumer identity for a component version repository based on a given repository specification.
	// The identity is used to look up credentials for accessing the repository.
	GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error)
	// GetComponentVersionRepository retrieves a component version repository based on a given repository specification.
	GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (oci.ComponentVersionRepository, error)
}
