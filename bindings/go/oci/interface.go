package oci

import (
	"context"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/internal/fetch"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	"ocm.software/open-component-model/bindings/go/repository"
)

// LocalBlob represents a blob that is stored locally in the OCI repository.
// It provides methods to access the blob's metadata and content.
type LocalBlob fetch.LocalBlob

// ComponentVersionRepository defines the interface for storing and retrieving OCM component versions
// and their associated resources in a Store.
type ComponentVersionRepository interface {
	repository.ComponentVersionRepository
	repository.HealthCheckable
	ResourceDigestProcessor
}

// ResourceRepository defines the interface for storing and retrieving OCM resources
// independently of component versions from a store implementation.
// When credentials are required to access the repository, they must be provided
// and can be retrieved through the credentials.Resolver or passed in directly.
// You should typically use the credentials.Graph to resolve credentials for a resource
// by its consumer identity.
type ResourceRepository interface {
	repository.ResourceRepository
}

// SourceRepository defines the interface for storing and retrieving OCM sources
// independently of component versions from a store implementation.
// TODO https://github.com/open-component-model/ocm-project/issues/857 also provide credentials in UploadSource/DownloadSource
type SourceRepository interface {
	repository.SourceRepository
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

	// Ping does a healthcheck for the underlying Store. The implementation varies based on the implementing
	// technology.
	Ping(ctx context.Context) error
}
