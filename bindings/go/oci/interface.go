package oci

import (
	"context"
	"fmt"

	"oras.land/oras-go/v2/errdef"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/internal/fetch"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ErrNotFound is returned when a component version or resource is not found in the OCI repository.
// We alias here so it can be coded against as a contract.
var ErrNotFound = errdef.ErrNotFound

// LocalBlob represents a blob that is stored locally in the OCI repository.
// It provides methods to access the blob's metadata and content.
type LocalBlob fetch.LocalBlob

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
	// If the component version does not exist, it returns ErrNotFound.
	GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error)

	// ListComponentVersions lists all versions for a given component.
	// Returns a list of version strings, sorted on best effort by loose semver specification.
	// Note: Listing of Component Versions does not directly translate to an OCI Call.
	// Thus there are two approaches to list component versions:
	// - Listing all tags in the OCI repository and filtering them based on the resolved media type / artifact type
	// - Listing all referrers of the component index and filtering them based on the resolved media type / artifact type
	//
	// For more information on Referrer support, see
	// https://github.com/opencontainers/distribution-spec/blob/v1.1.0/spec.md#listing-referrers
	ListComponentVersions(ctx context.Context, component string) ([]string, error)

	// CheckHealth checks if the repository is accessible and properly configured.
	// This method verifies that the underlying OCI registry is reachable and that authentication
	// is properly configured. It performs a lightweight check without modifying the repository.
	CheckHealth(ctx context.Context) error

	LocalResourceRepository
	LocalSourceRepository
	ResourceDigestProcessor
}

type LocalResourceRepository interface {
	// AddLocalResource adds a local [descriptor.Resource] to the repository.
	// The resource must be referenced in the [descriptor.Descriptor].
	// Resources for non-existent component versions may be stored but may be removed during garbage collection.
	// The Resource given is identified later on by its own Identity ([descriptor.Resource.ToIdentity]) and a collection of a set of reserved identity values
	// that can have a special meaning.
	AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (newRes *descriptor.Resource, err error)

	// GetLocalResource retrieves a local [descriptor.Resource] from the repository.
	// The [runtime.Identity] must match a resource in the [descriptor.Descriptor].
	GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (LocalBlob, *descriptor.Resource, error)
}

type LocalSourceRepository interface {
	// AddLocalSource adds a local [descriptor.Source] to the repository.
	// The source must be referenced in the [descriptor.Descriptor].
	// Sources for non-existent component versions may be stored but may be removed during garbage collection.
	// The Source given is identified later on by its own Identity ([descriptor.Source.ToIdentity]) and a collection of a set of reserved identity values
	// that can have a special meaning.
	AddLocalSource(ctx context.Context, component, version string, res *descriptor.Source, content blob.ReadOnlyBlob) (newRes *descriptor.Source, err error)

	// GetLocalSource retrieves a local [descriptor.Source] from the repository.
	// The [runtime.Identity] must match a source in the [descriptor.Descriptor].
	GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (LocalBlob, *descriptor.Source, error)
}

// ResourceRepository defines the interface for storing and retrieving OCM resources
// independently of component versions from a Store Implementation
type ResourceRepository interface {
	// UploadResource uploads a [descriptor.Resource] to the repository.
	// Returns the updated resource with repository-specific information.
	// The resource must be referenced in the component descriptor.
	// Note that UploadResource is special in that it considers both
	// - the Access from [descriptor.Resource.Access]
	// - the Target Access from the given target specification
	// It might be that during the upload, the source pointer may be updated with information gathered during upload
	// (e.g. digest, size, etc).
	//
	// The content of form blob.ReadOnlyBlob is expected to be a (optionally gzipped) tar archive that can be read with
	// tar.ReadOCILayout, which interprets the blob as an OCILayout.
	//
	// The given OCI Layout MUST contain the resource described in source with an v1.OCIImage specification,
	// otherwise the upload will fail
	UploadResource(ctx context.Context, targetAccess runtime.Typed, source *descriptor.Resource, content blob.ReadOnlyBlob) (resourceAfterUpload *descriptor.Resource, err error)

	// DownloadResource downloads a [descriptor.Resource] from the repository.
	// THe resource MUST contain a valid v1.OCIImage specification that exists in the Store.
	// Otherwise, the download will fail.
	//
	// The blob.ReadOnlyBlob returned will always be an OCI Layout, readable by [tar.ReadOCILayout].
	// For more information on the download procedure, see [tar.NewOCILayoutWriter].
	DownloadResource(ctx context.Context, res *descriptor.Resource) (content blob.ReadOnlyBlob, err error)
}

type SourceRepository interface {
	// UploadSource uploads a [descriptor.Source] to the repository.
	// Returns the updated source with repository-specific information.
	// The source must be referenced in the component descriptor.
	// Note that UploadSource is special in that it considers both
	// - the Access from [descriptor.Source.Access]
	// - the Target Access from the given target specification
	// It might be that during the upload, the source pointer may be updated with information gathered during upload
	// (e.g. digest, size, etc).
	//
	// The content of form blob.ReadOnlyBlob is expected to be a (optionally gzipped) tar archive that can be read with
	// tar.ReadOCILayout, which interprets the blob as an OCILayout.
	//
	// The given OCI Layout MUST contain the source described in source with an v1.OCIImage specification,
	// otherwise the upload will fail
	UploadSource(ctx context.Context, targetAccess runtime.Typed, source *descriptor.Source, content blob.ReadOnlyBlob) (sourceAfterUpload *descriptor.Source, err error)

	// DownloadSource downloads a [descriptor.Source] from the repository.
	// THe resource MUST contain a valid v1.OCIImage specification that exists in the Store.
	// Otherwise, the download will fail.
	//
	// The blob.ReadOnlyBlob returned will always be an OCI Layout, readable by [tar.ReadOCILayout].
	// For more information on the download procedure, see [tar.NewOCILayoutWriter].
	DownloadSource(ctx context.Context, res *descriptor.Source) (content blob.ReadOnlyBlob, err error)
}

type ResourceDigestProcessor interface {
	// ProcessResourceDigest processes, verifies and appends the [*descriptor.Resource.Digest] with information fetched
	// from the repository.
	// Under certain circumstances, it can also process the [*descriptor.Resource.Access] of the resource,
	// e.g. to ensure that the digest is pinned after digest information was appended.
	// As a result, after processing, the access MUST always reference the content described by the digest and cannot be mutated.
	ProcessResourceDigest(ctx context.Context, res *descriptor.Resource) (*descriptor.Resource, error)
}

// Resolver defines the interface for resolving references to OCI stores.
type Resolver interface {
	// StoreForReference resolves a reference to a Store.
	// Each reference can resolve to a different store.
	// Note that multiple component versions might share the same store
	StoreForReference(ctx context.Context, reference string) (spec.Store, error)

	// ComponentVersionReference returns a unique reference for a component version.
	ComponentVersionReference(ctx context.Context, component, version string) string

	// Reference resolves a reference string to a fmt.Stringer whose "native"
	// format represents a valid reference that can be used for a given store returned
	// by StoreForReference.
	Reference(reference string) (fmt.Stringer, error)

	// Ping does a healthcheck for the underlying Store. The implementation varies based on the implementing
	// technology.
	Ping(ctx context.Context) error
}
