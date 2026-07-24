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
	return r.ReferenceCache.Resolve(ctx, r.Repository, r.referenceNamespace(), reference)
}

// referenceNamespace returns a stable per-repository identifier used
// to scope reference-cache keys. It pulls registry + repository from
// the embedded *remote.Repository so two repositories that share the
// same short reference do not collide. Falls back to an empty
// namespace if either component is unset.
func (r *Repository) referenceNamespace() string {
	if r.Repository == nil {
		return ""
	}
	ref := r.Reference
	if ref.Registry == "" || ref.Repository == "" {
		return ""
	}
	return ref.Registry + "/" + ref.Repository
}

// Unwrap returns the embedded [*remote.Repository] so consumers that
// type-assert on the underlying store (e.g. global-store detection in
// internal/pack) can see through the cache decorator.
func (r *Repository) Unwrap() content.Storage {
	return r.Repository
}

// Untag implements [content.Untagger] by delegating to the underlying
// remote repository so alias deletion keeps working when the cache
// decorator is in the store chain.
func (r *Repository) Untag(ctx context.Context, reference string) error {
	return (&remotestore.RemoteStore{Repository: r.Repository}).Untag(ctx, reference)
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
