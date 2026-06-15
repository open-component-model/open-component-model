// Package cache provides two disk-backed, TTL+LRU caches that layer
// on top of an oras [*remote.Repository] via [Repository]:
//
//   - [BlobCache] keys cached blobs by digest and stores each blob as
//     a file under `<Options.Dir>/blobs/<algo>/<hex>`. Its primary
//     consumer is [BlobCache.Fetch], which mirrors the oras-go
//     internal/cas/proxy.go pattern: the first Fetch of a descriptor
//     reads from upstream and tees the bytes to disk; subsequent
//     Fetches for the same digest serve the on-disk file. Push, Tag,
//     Resolve, FetchReference, and Exists on the wrapping
//     [Repository] are pure passthroughs.
//
//   - [ReferenceCache] keys cached resolves by (namespace, reference)
//     so two repositories that happen to share a short reference
//     (e.g. the tag "v1") cannot collide. Each namespace's entries
//     are persisted to its own file under
//     `<Options.Dir>/refs/<fnv1a(namespace)>.json`; the FNV-1a hash
//     is used purely to derive a compact filename — the canonical
//     namespace string is stored inside the snapshot body, so any
//     unlikely hash collision is recoverable.
//
// Both caches share an [Options] struct so a caller can configure
// limits once and instantiate either or both. The blob-only fields
// ([Options.MaxBlobSize] and [Options.Accept]) are ignored by
// [ReferenceCache].
//
// Scope and intent:
//
//   - [BlobCache] is intentionally narrow: by default it only caches
//     OCI/Docker manifests and OCM component-descriptor blobs
//     (configurable via [Options.Accept]). Layer blobs and arbitrary
//     octet streams are not cached so disk usage stays bounded by
//     descriptor metadata size.
//
//   - [ReferenceCache] is unconditional within its namespace: every
//     successful upstream resolve is recorded.
//
// Lifecycle and isolation:
//
//   - Each cache owns one or more subdirectories of [Options.Dir]
//     ([BlobCache]: `blobs/`; [ReferenceCache]: `refs/`). Sharing the
//     same Dir between both caches is therefore safe; they do not
//     collide on the filesystem.
//   - Cache directories are persistent: callers should pass a stable
//     path so a future run with the same Dir reuses the existing
//     content. Neither cache provides a Close method; both reseed
//     their LRUs from the on-disk state on construction.
//   - Eviction (LRU overflow or TTL expiry) deletes the on-disk file
//     for blobs and rewrites/removes the per-namespace snapshot for
//     references.
//   - All public methods are safe for concurrent use. Concurrent
//     [BlobCache.Populate] calls for the same digest race on the
//     final [os.Rename]; the last winner wins and both produce
//     byte-identical content because the digest is the cache key.
//
// Integrity:
//
//   - [BlobCache] trusts the descriptor digest as the cache key and
//     does not verify content. This mirrors oras-go's proxy. Integrity
//     verification is expected to live one layer above (e.g. oras
//     VerifyReader paths).
//   - [ReferenceCache] trusts upstream's resolved descriptor and
//     records it verbatim.
package cache
