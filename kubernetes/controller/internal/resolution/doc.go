// Package resolution contains the component resolution service used by the controller reconcilers.
//
// # Resolution Flow
//
// The following actions are taken once a resolution request is received through a reconciler:
//
//	Input: CacheBackedRepository.GetComponentVersion(component, version)
//		↓
//	Build cache key from (configHash, repoSpec, component, version, verifications, digest)
//		↓
//	WorkerPool.GetComponentVersion(opts)
//		├─ Check cache (fast path) → return if hit
//		├─ Check inProgress map   → return ErrResolutionInProgress if already queued
//		├─ Add to inProgress + enqueue work item
//		└─ Return ErrResolutionInProgress (controller will be re-triggered via event source)
//		↓
//	Worker goroutine (one of N concurrent workers)
//		├─ Fetch descriptor from OCI repository
//		├─ If verifications are set:
//		│   ├─ Check IsSafelyDigestible → return (desc, ErrNotSafelyDigestible) if not
//		│   ├─ For each verification:
//		│   │   ├─ Find matching signature in descriptor
//		│   │   ├─ Verify digest matches descriptor normalisation
//		│   │   └─ Verify signature using signing handler + public key
//		│   └─ Fail on any verification error
//		├─ Store result (descriptor + error) in cache
//		└─ Notify all waiting requesters via event channel
//
// # Cache Key Generation
//
// The cache key is an FNV-1a 64-bit hash of the following inputs, canonicalized using JCS (RFC 8785)
// where applicable:
//
//	key = FNV-1a(configHash | Canonical(repoSpec) | component | version | Canonical(verifications) | digest)
//
// Verifications are sorted by signature name before canonicalization to ensure deterministic keys.
// The cache has a configurable TTL (default 30 minutes) and unlimited size. Entries are evicted by
// a background goroutine when the TTL expires.
//
// # Verification and Cache Key Separation
//
// Verifications (signature name + public key bytes) and digest specs are part of the cache key.
// This means the same component+version produces different cache entries depending on the
// verification context:
//
//   - No verification, no digest:        key = hash(..., [], null)       → "unverified"
//   - Verification with sig A + key X:   key = hash(..., [{A,X}], null) → "verified"
//   - Verification with sig A + key Y:   key = hash(..., [{A,Y}], null) → "verified"
//   - Child via reference path digest D: key = hash(..., [], D)         → "verified"
//
// This design ensures that an unverified cache entry is never served to a requester that
// asked for verification, and vice versa. However, it also means that different verification
// configurations for the same component version result in separate cache entries, each requiring
// its own resolution and verification cycle.
//
// # Integrity Chain for Component References
//
// When a resource is resolved through a reference path (nested component references), the
// integrity chain works as follows:
//
//  1. The parent component is resolved with verifications from the Component CR.
//     The signature is verified in the worker pool before caching.
//  2. The parent descriptor contains component references, each with a digest computed
//     over the referenced component's normalised form.
//  3. The digest from the matching reference is set on the CacheBackedRepository used
//     to resolve the child component, becoming part of the child's cache key.
//  4. The child component itself is NOT signature-verified. Its integrity is guaranteed
//     transitively: the parent was verified, and the parent's reference digest covers the
//     child's normalised descriptor.
//
// # Error Caching Behavior
//
//   - Verification failures (wrong key, missing signature): the error is cached but removed
//     on the next cache read, allowing the controller to retry immediately on the next
//     reconcile cycle. This means a misconfigured verification produces continuous OCI
//     traffic rather than backing off.
//   - ErrNotSafelyDigestible: the descriptor AND the error are both cached and persist for
//     the full TTL. Controllers log an event but continue using the descriptor. This means
//     components that are not safely digestible are used unverified even when verification
//     is requested.
//   - ErrResolutionInProgress: never cached. Returned synchronously when resolution is
//     already queued.
//
// The process and the service are described by ADR docs/adr/0009_controller_v2_lib_migration.md.
package resolution
