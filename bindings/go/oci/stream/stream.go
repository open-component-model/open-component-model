package stream

import (
	"context"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ResourceStream is a lazy handle to OCI content.
// It implements content.ReadOnlyStorage so it can be passed
// directly to oras.CopyGraph or consumed blob-by-blob.
// No data is fetched until Fetch or Materialize is called.
type ResourceStream interface {
	content.ReadOnlyStorage

	// Root returns the top-level descriptor (manifest or index).
	Root() ocispec.Descriptor

	// Predecessors returns the nodes directly pointing to node (its parents, e.g.
	// referrer manifests), making the stream a content.ReadOnlyGraphStorage that can
	// be the source of an oras.ExtendedCopyGraph. ExtendedCopyGraph walks these up
	// from Root to copy referrers alongside it; pair it with a FindPredecessors
	// filter to restrict which referrers travel.
	Predecessors(ctx context.Context, node ocispec.Descriptor) ([]ocispec.Descriptor, error)

	// Materialize produces a ReadOnlyBlob (OCI layout tar) for legacy consumers.
	// This is the only code path that creates a tar file.
	Materialize(ctx context.Context) (blob.ReadOnlyBlob, error)
}

// ResourceRepository extends the generic ResourceRepository with
// OCI-native streaming. Only implemented by OCI-backed repositories.
type ResourceRepository interface {
	repository.ResourceRepository

	// DownloadResourceStream returns a lazy store handle and root descriptor.
	// No data is downloaded yet — content streams on demand via Fetch calls.
	DownloadResourceStream(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (ResourceStream, error)

	// UploadResourceStream writes content from a ResourceStream into this repository.
	// Internally uses oras.CopyGraph for blob-by-blob streaming with deduplication.
	UploadResourceStream(ctx context.Context, resource *descriptor.Resource, stream ResourceStream, credentials runtime.Typed) (*descriptor.Resource, error)
}
