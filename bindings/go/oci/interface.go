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
// independently of component versions from a Store Implementation
type ResourceRepository interface {
	repository.ResourceRepository
}

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
