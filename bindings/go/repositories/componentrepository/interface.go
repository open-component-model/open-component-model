package componentrepository

import (
	"context"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	Realm = "componentrepository"
)

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

	// GetLocalResource retrieves a local [descriptor.Resource] from the repository.
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

	// GetLocalSource retrieves a local [descriptor.Source] from the repository.
	// The [runtime.Identity] must match a source in the [descriptor.Descriptor].
	GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error)
}

type CredentialProvider interface {
	// Resolve attempts to resolve credentials for the given identity.
	Resolve(ctx context.Context, identity runtime.Identity) (map[string]string, error)
}

// NotFoundError is an error type that indicates a requested component version
// was not found. NotFoundError is independent of the underlying repository implementation.
// It is supposed to wrap the original technology-specific error and to provide a
// technology-agnostic API to check for not found errors.
type NotFoundError struct {
	msg string
	err error
}

func (e *NotFoundError) Error() string {
	return e.msg
}

func (e *NotFoundError) Unwrap() error {
	return e.err
}

func NewNotFoundError(msg string, err error) *NotFoundError {
	return &NotFoundError{
		msg: msg,
		err: err,
	}
}
