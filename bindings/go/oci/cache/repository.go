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

// store is an unexported alias for the chunked remote store so the decorator
// embeds a single backing store without surfacing the internal type as an
// exported field.
type store = remotestore.RemoteStore

// repository decorates the chunked remote [store] with optional [BlobCache] and
// [ReferenceCache] layers. The raw [*remote.Repository] is reached as
// r.Repository, promoted through the embedded store.
//
// Embedding the store keeps chunked/streaming Push, Untag, Referrers and the
// promoted oras type assertions ([registry.TagLister], [registry.ReferrerLister],
// Blobs()) working through a single representation of the remote repository; the
// overrides below only add the cache reads. Either cache may be nil, degrading
// that override to a passthrough.
type repository struct {
	*store

	BlobCache      *BlobCache
	ReferenceCache *ReferenceCache
}

var (
	_ spec.Store       = (*repository)(nil)
	_ content.Untagger = (*repository)(nil)
)

// Fetch consults [repository.BlobCache], then delegates to the backing
// repository. A nil BlobCache is a passthrough.
func (r *repository) Fetch(ctx context.Context, target ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	return r.BlobCache.Fetch(ctx, r.Repository, target)
}

// Resolve consults [repository.ReferenceCache], then delegates to the backing
// repository. The cache key is namespaced by registry/repository so a short
// reference (e.g. "v1") cannot collide across repositories. A nil
// ReferenceCache is a passthrough.
func (r *repository) Resolve(ctx context.Context, reference string) (ociImageSpecV1.Descriptor, error) {
	if r.ReferenceCache == nil {
		return r.Repository.Resolve(ctx, reference)
	}
	ref, err := r.ParseReference(reference)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, err
	}
	return r.ReferenceCache.Resolve(ctx, r.Repository, ref)
}

// Unwrap exposes the raw [*remote.Repository] so consumers that type-assert on
// the raw store (e.g. global-store detection in internal/pack) see through the
// decorator.
func (r *repository) Unwrap() content.Storage {
	return r.Repository
}

// Untag removes the tag via the backing store, then invalidates the reference
// cache so a restart does not resurrect the stale tag→descriptor mapping.
func (r *repository) Untag(ctx context.Context, reference string) error {
	if err := r.store.Untag(ctx, reference); err != nil {
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

// Tag tags desc on the backing repository, then refreshes the reference cache so
// the mutable tag resolves to the new descriptor instead of a stale digest. A
// nil ReferenceCache is a passthrough.
func (r *repository) Tag(ctx context.Context, desc ociImageSpecV1.Descriptor, reference string) error {
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
	backing := &remotestore.RemoteStore{Repository: repo, ChunkSize: chunkSize, ChunkThreshold: chunkThreshold}
	if blob == nil && refs == nil {
		return backing
	}
	return &repository{
		store:          backing,
		BlobCache:      blob,
		ReferenceCache: refs,
	}
}
