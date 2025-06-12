package provider

import (
	"context"

	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ComponentVersionRepositoryProvider defines the contract for providers that can retrieve
// and manage component version repositories. It supports different types of repository
// specifications, including OCI and CTF repositories.
type ComponentVersionRepositoryProvider interface {
	// GetComponentVersionRepositoryCredentialConsumerIdentity retrieves the consumer identity
	// for a component version repository based on a given repository specification.
	//
	// The identity can be used to look up credentials for accessing the repository and typically
	// includes information like hostname, port and/or path.
	GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error)

	// GetComponentVersionRepository retrieves a component version repository based on a given
	// repository specification and credentials.
	//
	// This method is responsible for:
	// - Validating the repository specification
	// - Setting up the repository with appropriate credentials
	// - Configuring caching and other repository options
	GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (oci.ComponentVersionRepository, error)
}
