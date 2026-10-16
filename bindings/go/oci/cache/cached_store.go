package cache

import (
	"context"
	"io"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/oci/internal/remotestore"
	"ocm.software/open-component-model/bindings/go/oci/spec"
)

// cachedStore is a caching store decorator stacked over a
// [*remotestore.RemoteStore]. It adds optional [BlobCache] and
// [ReferenceCache] reads in front of the backing remote store; the raw
// [*remote.Repository] is reached as r.Repository, promoted through the
// embedded store.
//
// Embedding the concrete RemoteStore keeps chunked/streaming Push, Untag,
// Referrers and the promoted oras type assertions ([registry.TagLister],
// [registry.ReferrerLister], Blobs()) working through a single representation
// of the remote store; the overrides below only add the cache reads. Either
// cache may be nil, degrading that override to a passthrough.
type cachedStore struct {
	*remotestore.RemoteStore

	BlobCache      *BlobCache
	ReferenceCache *ReferenceCache
}

var (
	_ spec.Store       = (*cachedStore)(nil)
	_ content.Untagger = (*cachedStore)(nil)
)

// Fetch consults [cachedStore.BlobCache], then delegates to the backing store.
// A nil BlobCache is a passthrough.
func (r *cachedStore) Fetch(ctx context.Context, target ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	return r.BlobCache.Fetch(ctx, r.Repository, target)
}

// Resolve consults [cachedStore.ReferenceCache], then delegates to the backing
// store. The cache key is namespaced by registry/repository so a short
// reference (e.g. "v1") cannot collide across repositories. A nil
// ReferenceCache is a passthrough.
func (r *cachedStore) Resolve(ctx context.Context, reference string) (ociImageSpecV1.Descriptor, error) {
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
func (r *cachedStore) Unwrap() content.Storage {
	return r.Repository
}

// Untag removes the tag via the backing store, then invalidates the reference
// cache so a restart does not resurrect the stale tag→descriptor mapping.
func (r *cachedStore) Untag(ctx context.Context, reference string) error {
	if err := r.RemoteStore.Untag(ctx, reference); err != nil {
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

// Tag tags desc on the backing store, then refreshes the reference cache so the
// mutable tag resolves to the new descriptor instead of a stale digest. A nil
// ReferenceCache is a passthrough.
func (r *cachedStore) Tag(ctx context.Context, desc ociImageSpecV1.Descriptor, reference string) error {
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

// Wrap stacks the configured caches over backing and owns the
// wrap-or-passthrough decision: with both caches nil it returns backing
// unchanged, so the chunked/streaming store is handed out directly; otherwise it
// returns a cachedStore whose cache reads sit in front of backing. Embedding the
// concrete RemoteStore keeps chunked and streaming push reachable through the
// decorator.
func Wrap(backing *remotestore.RemoteStore, blob *BlobCache, refs *ReferenceCache) spec.Store {
	if blob == nil && refs == nil {
		return backing
	}
	return &cachedStore{
		RemoteStore:    backing,
		BlobCache:      blob,
		ReferenceCache: refs,
	}
}
