package cache

import (
	"context"
	"io"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/oci/internal/remotestore"
	"ocm.software/open-component-model/bindings/go/oci/spec"
)

// CachedStore is a store decorator that stacks optional [BlobCache] and
// [ReferenceCache] layers over a backing [*remotestore.RemoteStore].
//
// Embedding RemoteStore preserves chunked push and the backing store's other
// capabilities when caching is enabled.
//
// Either cache may be nil; construct via [Wrap].
type CachedStore struct {
	*remotestore.RemoteStore
	BlobCache      *BlobCache
	ReferenceCache *ReferenceCache
}

var (
	_ spec.Store       = (*CachedStore)(nil)
	_ content.Untagger = (*CachedStore)(nil)
)

func (r *CachedStore) Fetch(ctx context.Context, target ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	return r.BlobCache.Fetch(ctx, r.Repository, target)
}

// Resolve namespaces references through ParseReference before consulting
// [ReferenceCache.Resolve], preventing collisions between repositories.
func (r *CachedStore) Resolve(ctx context.Context, reference string) (ociImageSpecV1.Descriptor, error) {
	if r.ReferenceCache == nil {
		return r.Repository.Resolve(ctx, reference)
	}
	ref, err := r.ParseReference(reference)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, err
	}
	return r.ReferenceCache.Resolve(ctx, r.Repository, ref)
}

// Unwrap exposes the raw *remote.Repository so oci/internal/pack's global-store detection can see through the cache decorator.
func (r *CachedStore) Unwrap() content.Storage {
	return r.Repository
}

// Untag invalidates the reference cache after successful remote deletion so
// stale tag mappings cannot reappear after restart.
func (r *CachedStore) Untag(ctx context.Context, reference string) error {
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

// Tag refreshes the reference cache after a successful remote tag update so
// mutable tags do not retain stale digests until TTL expiry.
func (r *CachedStore) Tag(ctx context.Context, desc ociImageSpecV1.Descriptor, reference string) error {
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

// Wrap returns backing unchanged when both caches are nil, otherwise
// wrapping it in a [CachedStore].
func Wrap(backing *remotestore.RemoteStore, blob *BlobCache, refs *ReferenceCache) spec.Store {
	if blob == nil && refs == nil {
		return backing
	}
	return &CachedStore{RemoteStore: backing, BlobCache: blob, ReferenceCache: refs}
}
