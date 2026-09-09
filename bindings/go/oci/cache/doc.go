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
//     `<Options.Dir>/refs/<sha256(namespace)>.json`; the SHA-256
//     digest is used purely to derive a compact, collision-free
//     filename — the canonical namespace string is stored inside the
//     snapshot body so a reseed recovers the exact key without
//     trusting the filename to preserve it.
//
// Both caches share an [Options] struct so a caller can configure
// limits once and instantiate either or both. The blob-only fields
// ([Options.MaxBlobSize] and [Options.Accept]) are ignored by
// [ReferenceCache].
//
// Credential scoping:
//
//   - [ScopeKey] returns a short, filesystem-safe hash of the
//     (registry identity, credential) pair. Callers that share a
//     cache directory across multiple credential sets should compose
//     [Options.Dir] with the [ScopeKey] so tag→descriptor mappings
//     resolved under one credential cannot leak into another. The
//     content-addressed [BlobCache] does not need this scoping
//     because blobs are identified by digest, but the tag-mutable
//     [ReferenceCache] does — see the provider package for the
//     canonical wiring.
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
//     [BlobCache.Fetch] calls for the same digest are collapsed via
//     singleflight into a single upstream round-trip; the shared
//     bytes are then persisted to disk (best-effort) and each waiter
//     is served its own reader over identical content because the
//     digest is the cache key.
//
// Integrity:
//
//   - [BlobCache] verifies content on the cache-miss path by fetching
//     via [content.FetchAll], which streams the upstream reader
//     through a [content.VerifyReader] and rejects bytes that do not
//     hash to the descriptor's digest. Only verified bytes are
//     persisted to disk and served to callers.
//   - On a cache hit the bytes are already keyed by digest under a
//     directory owned by this process; the file's identity is trusted
//     without a re-hash.
//   - [ReferenceCache] trusts upstream's resolved descriptor and
//     records it verbatim.
package cache
