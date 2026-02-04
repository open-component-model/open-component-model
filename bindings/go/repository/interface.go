package repository

import (
	"context"
	"errors"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ErrNotFound is an error type that indicates a requested component version
// was not found. NotFoundError is independent of the underlying repository implementation.
// It is supposed to be joined with the original technology-specific error to provide a
// technology-agnostic API to check for not found errors.
var ErrNotFound = errors.New("component version not found")

// ComponentVersionRepositoryProvider defines the contract for providers that can retrieve
// and manage component version repositories. It supports different types of repository
// specifications.
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
	GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (ComponentVersionRepository, error)

	// GetJSONSchemaForRepositorySpecification returns the JSON schema for a given repository specification type.
	GetJSONSchemaForRepositorySpecification(typ runtime.Type) ([]byte, error)
}

// ComponentVersionRepository defines the interface for storing and retrieving OCM component versions
// and their associated resources in a Store.
type ComponentVersionRepository interface {
	// AddComponentVersion adds a new component version to the repository.
	// If a component version already exists, it will be updated with the new descriptor.
	// The descriptor internally will be serialized via the runtime package.
	// The descriptor MUST have its target Name and Version already set as they are used to identify the target
	// Location in the Store.
	AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error

	// GetComponentVersion retrieves a component version from the repository.
	// Returns the descriptor for the given component name and version.
	// If the component version does not exist, it returns NotFoundError.
	GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error)

	// ListComponentVersions lists all versions for a given component.
	// Returns a list of version strings, sorted on the best effort by loose semver specification.
	// Thus, there are two approaches to listing component versions:
	// - Listing all tags in a repository and filtering them based on the resolved media type / artifact type
	// - Listing all referrers of the component index and filtering them based on the resolved media type / artifact type
	ListComponentVersions(ctx context.Context, component string) ([]string, error)

	LocalResourceRepository
	LocalSourceRepository
}

// LocalResourceRepository defines the interface for managing local resources within a component version repository.
// Local resources are artifacts that are stored directly in the repository rather than referenced externally.
type LocalResourceRepository interface {
	// AddLocalResource adds a local [descriptor.Resource] to the repository.
	// The resource must be referenced in the [descriptor.Descriptor].
	// Resources for non-existent component versions may be stored but may be removed during garbage collection.
	// The Resource given is identified later on by its own Identity ([descriptor.Resource.ToIdentity]) and a collection of a set of reserved identity values
	// that can have a special meaning.
	AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error)

	// GetLocalResource retrieves and verifies the integrity of a local [descriptor.Resource] from the repository.
	// The [runtime.Identity] must match a resource in the [descriptor.Descriptor].
	GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error)
}

// LocalSourceRepository defines the interface for managing local sources within a component version repository.
// Local sources are source code artifacts that are stored directly in the repository rather than referenced externally.
type LocalSourceRepository interface {
	// AddLocalSource adds a local [descriptor.Source] to the repository.
	// The source must be referenced in the [descriptor.Descriptor].
	// Sources for non-existent component versions may be stored but may be removed during garbage collection.
	// The Source given is identified later on by its own Identity ([descriptor.Source.ToIdentity]) and a collection of a set of reserved identity values
	// that can have a special meaning.
	AddLocalSource(ctx context.Context, component, version string, src *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error)

	// GetLocalSource retrieves and verifies the integrity of a local [descriptor.Source] from the repository.
	// The [runtime.Identity] must match a source in the [descriptor.Descriptor].
	GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error)
}

// ResourceRepository defines the interface for storing and retrieving OCM resources
// independently of component versions from a store implementation.
type ResourceRepository interface {
	// GetResourceCredentialConsumerIdentity resolves the identity of the given [descriptor.Resource] to use for credential resolution.
	GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error)
	// UploadResource uploads a [descriptor.Resource] to the repository.
	// Returns the updated resource with repository-specific information.
	// The resource must be referenced in the component descriptor.
	// The credentials map must contain necessary authentication information to access the resource.
	UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, credentials map[string]string) (resourceAfterUpload *descriptor.Resource, err error)
	// DownloadResource downloads and verifies the integrity of a [descriptor.Resource] from the repository.
	// The credentials map must contain necessary authentication information to access the resource.
	DownloadResource(ctx context.Context, res *descriptor.Resource, credentials map[string]string) (blob.ReadOnlyBlob, error)
}

// SourceRepository defines the interface for storing and retrieving OCM sources
// independently of component versions from a store implementation.
type SourceRepository interface {
	// UploadSource uploads a [descriptor.Source] to the repository.
	// Returns the updated source with repository-specific information.
	// The source must be referenced in the component descriptor.
	UploadSource(ctx context.Context, targetAccess runtime.Typed, source *descriptor.Source, content blob.ReadOnlyBlob) (sourceAfterUpload *descriptor.Source, err error)

	// DownloadSource downloads and verifies the integrity of a [descriptor.Source] from the repository.
	DownloadSource(ctx context.Context, res *descriptor.Source) (content blob.ReadOnlyBlob, err error)
}

// ResourceDigestProcessor defines the interface for processing resource digests.
type ResourceDigestProcessor interface {
	// ProcessResourceDigest processes, verifies and appends the [*descriptor.Resource.Digest] with information fetched
	// from the repository.
	// Under certain circumstances, it can also process the [*descriptor.Resource.Access] of the resource,
	// e.g. to ensure that the digest is pinned after digest information was appended.
	// As a result, after processing, the access MUST always reference the content described by the digest and cannot be mutated.
	ProcessResourceDigest(ctx context.Context, res *descriptor.Resource) (*descriptor.Resource, error)
}

// HealthCheckable is an optional interface that can be implemented by a
// component version repository.
type HealthCheckable interface {
	// CheckHealth checks if the repository is accessible and properly configured.
	// This method verifies that the underlying OCI registry is reachable and that authentication
	// is properly configured. It performs a lightweight check without modifying the repository.
	CheckHealth(ctx context.Context) error
}

// ComponentVersionRepositorySpecProvider defines the interface for resolving repository specifications
// based on a given component identity.
type ComponentVersionRepositorySpecProvider interface {
	// GetRepositorySpec returns the repository specification for the given component identity.
	// It can use various strategies to determine the appropriate repository specification.
	GetRepositorySpec(ctx context.Context, componentIdentity runtime.Identity) (runtime.Typed, error)
}

// ComponentLister defines the interface for listing OCM components in an OCI store.
// It is an optional interface that can be implemented to expose the contents of a specific store,
// e.g. of a CTF archive, of a Docker catalog etc.
type ComponentLister interface {
	// ListComponents lists names of OCM components contained in the OCM store.
	//
	// If the underlying store implementation supports pagination, the callback function `fn` is called
	// for every page of the result. Otherwise, the complete list is retrieved and provided
	// to the callback function at once.
	//
	// The `last` parameter is the value of the last element of the previous page.
	// If `last` is NOT empty, the entries in the returned list start after the component name specified by `last`.
	// Otherwise, the results start from the top of the component list.
	// If the underlying store implementation does not support pagination, it may ignore this parameter,
	// and return the complete list.
	//
	// The signature is inspired by ORAS TagLister interface:
	// https://pkg.go.dev/oras.land/oras-go/v2/registry@v2.6.0#TagLister
	// See also:
	// https://distribution.github.io/distribution/spec/api/#tags-paginated
	ListComponents(ctx context.Context, last string, fn func(names []string) error) error
}
