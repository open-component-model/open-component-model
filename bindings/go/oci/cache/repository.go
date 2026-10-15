package cache

import (
	"context"
	"io"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"

	"ocm.software/open-component-model/bindings/go/oci/internal/remotestore"
	"ocm.software/open-component-model/bindings/go/oci/spec"
)

// Repository decorates an oras [*remote.Repository] with optional [BlobCache]
// and [ReferenceCache] layers while preserving chunked and streaming push.
//
// The embedded [*remote.Repository] keeps the promoted type assertions used
// elsewhere ([registry.TagLister], [registry.ReferrerLister], Blobs()) working.
// Push, PushStreaming and Untag go through the chunked store (see getStore).
// Either cache may be nil, degrading that override to a passthrough.
type Repository struct {
	*remote.Repository

	// store holds the chunk config and is built once by [ProxyRepository].
	// Unexported so the internal remotestore type stays out of the public API.
	store *remotestore.RemoteStore

	BlobCache      *BlobCache
	ReferenceCache *ReferenceCache
}

// getStore returns the store backing Push, PushStreaming and Untag. A bare
// struct literal leaves store nil, so it falls back to a monolithic store over
// the embedded repository to avoid a panic.
func (r *Repository) getStore() *remotestore.RemoteStore {
	if r.store != nil {
		return r.store
	}
	return &remotestore.RemoteStore{Repository: r.Repository}
}

// Push delegates to the chunked store; a zero chunk size falls back to a
// monolithic push.
func (r *Repository) Push(ctx context.Context, expected ociImageSpecV1.Descriptor, content io.Reader) error {
	return r.getStore().Push(ctx, expected, content)
}

// PushStreaming exposes the chunked streaming push so pack's
// [remotestore.StreamingPusher] assertion succeeds under caching.
func (r *Repository) PushStreaming(ctx context.Context, partial ociImageSpecV1.Descriptor, content io.Reader) (ociImageSpecV1.Descriptor, error) {
	return r.getStore().PushStreaming(ctx, partial, content)
}

// Fetch consults [Repository.BlobCache], then delegates to the embedded
// repository. A nil BlobCache is a passthrough.
func (r *Repository) Fetch(ctx context.Context, target ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	return r.BlobCache.Fetch(ctx, r.Repository, target)
}

// Resolve consults [Repository.ReferenceCache], then delegates to the embedded
// repository. The cache key is namespaced by registry/repository so a short
// reference (e.g. "v1") cannot collide across repositories. A nil
// ReferenceCache is a passthrough.
func (r *Repository) Resolve(ctx context.Context, reference string) (ociImageSpecV1.Descriptor, error) {
	if r.ReferenceCache == nil {
		return r.Repository.Resolve(ctx, reference)
	}
	ref, err := r.ParseReference(reference)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, err
	}
	return r.ReferenceCache.Resolve(ctx, r.Repository, ref)
}

// Unwrap returns the embedded [*remote.Repository] so consumers that type-assert
// on the raw store (e.g. global-store detection in internal/pack) see through
// the decorator.
func (r *Repository) Unwrap() content.Storage {
	return r.Repository
}

// Referrers delegates to [remotestore.Referrers], which falls back to the
// referrers tag schema when the registry answers the API with NAME_UNKNOWN.
func (r *Repository) Referrers(ctx context.Context, desc ociImageSpecV1.Descriptor, artifactType string, onReferrers func(referrers []ociImageSpecV1.Descriptor) error) error {
	return remotestore.Referrers(ctx, r.Repository, desc, artifactType, onReferrers)
}

// Predecessors delegates to [remotestore.Predecessors]; see [Repository.Referrers].
func (r *Repository) Predecessors(ctx context.Context, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
	return remotestore.Predecessors(ctx, r.Repository, desc)
}

// Untag delegates to the remote repository, then invalidates the reference cache
// so a restart does not resurrect the stale tag→descriptor mapping.
func (r *Repository) Untag(ctx context.Context, reference string) error {
	if err := r.getStore().Untag(ctx, reference); err != nil {
		return err
	}
	if r.ReferenceCache != nil {
		ref, err := r.ParseReference(reference)
		if err != nil {
			return err
		}
		r.ReferenceCache.Invalidate(ref)
	}
	return nil
}

// Tag tags desc on the remote repository, then refreshes the reference cache so
// the mutable tag resolves to the new descriptor instead of a stale digest. A
// nil ReferenceCache is a passthrough.
func (r *Repository) Tag(ctx context.Context, desc ociImageSpecV1.Descriptor, reference string) error {
	if err := r.Repository.Tag(ctx, desc, reference); err != nil {
		return err
	}
	if r.ReferenceCache != nil {
		ref, err := r.ParseReference(reference)
		if err != nil {
			return err
		}
		r.ReferenceCache.Add(ref, desc)
	}
	return nil
}

// ProxyRepository decorates repo with the configured caches, carrying the chunk
// config so chunked and streaming push survive the cache (chunkSize bytes per
// PATCH at or above chunkThreshold; a zero chunkSize is monolithic). With no
// cache it returns the chunked store directly. It takes [*remote.Repository] to
// keep the internal store type out of its signature.
func ProxyRepository(repo *remote.Repository, blob *BlobCache, refs *ReferenceCache, chunkSize, chunkThreshold int64) spec.Store {
	store := &remotestore.RemoteStore{Repository: repo, ChunkSize: chunkSize, ChunkThreshold: chunkThreshold}
	if blob == nil && refs == nil {
		return store
	}
	return &Repository{
		Repository:     repo,
		store:          store,
		BlobCache:      blob,
		ReferenceCache: refs,
	}
}
