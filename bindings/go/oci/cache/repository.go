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

// Repository decorates an oras [*remote.Repository] with optional
// [BlobCache] and [ReferenceCache] layers. The embedded
// [*remote.Repository] is exposed via Go field promotion so type
// assertions used elsewhere in the codebase
// ([registry.TagLister], [registry.ReferrerLister],
// `interface{ Blobs() registry.BlobStore }`) keep working unchanged.
//
// Either cache may be nil; the corresponding override degrades to a
// pure passthrough to the embedded *remote.Repository.
type Repository struct {
	*remote.Repository
	BlobCache      *BlobCache
	ReferenceCache *ReferenceCache
}

// Fetch consults [Repository.BlobCache] before delegating to the
// embedded [*remote.Repository]. See [BlobCache.Fetch] for the exact
// semantics: cache hit returns the on-disk file directly; miss
// performs the upstream fetch and tees into the cache. Layer blobs
// and other non-manifest media types pass through transparently.
//
// When BlobCache is nil, this is a direct passthrough.
func (r *Repository) Fetch(ctx context.Context, target ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	return r.BlobCache.Fetch(ctx, r.Repository, target)
}

// Resolve consults [Repository.ReferenceCache] before delegating to
// the embedded [*remote.Repository]. See [ReferenceCache.Resolve] for
// the exact semantics. Successful resolves are appended to the
// snapshot so they survive a process restart against the same Dir.
//
// The cache key is namespaced by the embedded *remote.Repository's
// registry/repository so two repositories that happen to share a
// short reference (e.g. the tag "v1") cannot collide.
//
// When ReferenceCache is nil, this is a direct passthrough.
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

// Unwrap returns the embedded [*remote.Repository] so consumers that
// type-assert on the underlying store (e.g. global-store detection in
// internal/pack) can see through the cache decorator.
func (r *Repository) Unwrap() content.Storage {
	return r.Repository
}

// Untag implements [content.Untagger] by delegating to the underlying
// remote repository so alias deletion keeps working when the cache
// decorator is in the store chain. On success it invalidates the
// reference cache entry so a restart does not resurrect the stale
// tag→descriptor mapping.
func (r *Repository) Untag(ctx context.Context, reference string) error {
	if err := (&remotestore.RemoteStore{Repository: r.Repository}).Untag(ctx, reference); err != nil {
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

// Tag associates reference with desc on the underlying remote
// repository and, on success, refreshes the reference cache so the
// mutable tag now resolves to the newly tagged descriptor. Without
// this the cache would keep serving a previously resolved digest for
// the tag until TTL expiry (OCI tags are mutable). A nil
// ReferenceCache degrades to a pure passthrough.
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

// ProxyRepository proxies the given repo with the configured caches
// when at least one is non-nil; otherwise it returns repo unchanged
// so the cache decorator only appears in the type chain when there
// is something to cache.
func ProxyRepository(repo *remote.Repository, blob *BlobCache, refs *ReferenceCache) spec.Store {
	if blob == nil && refs == nil {
		return repo
	}
	return &Repository{Repository: repo, BlobCache: blob, ReferenceCache: refs}
}
