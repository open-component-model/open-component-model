// Package resolution provides component resolution with verification and caching.
//
// # Resolution Flow
//
// When a resolution request is received:
//  1. Check cache (fast path)
//  2. If queued already, return ErrResolutionInProgress
//  3. Enqueue work and return ErrResolutionInProgress (controller retriggers via event)
//  4. Worker fetches descriptor, runs verifications if set, stores result in cache
//
// # Cache Key
//
// FNV-1a hash of: configHash | repoSpec | component | version | verifications | digest
//
// Verifications (signature name + public key) and digest specs are included in the key.
// This ensures different verification contexts produce separate cache entries.
// TTL is configurable (default 30 minutes); entries are evicted by background goroutine.
//
// # Verification Contexts
//
// Different verification configs produce separate cache entries:
//   - No verification, no digest:        → "unverified"
//   - Verification with sig A + key X:   → "verified"
//   - Child via reference path digest D: → "verified"
//
// This prevents serving unverified entries to requesters that asked for verification.
//
// # Component Reference Integrity
//
// For nested component references:
//  1. Parent is resolved with verifications from Component CR
//  2. Parent descriptor contains references with computed digests
//  3. Child's cache key includes parent's reference digest
//  4. Child is NOT signature-verified; integrity is transitive via parent's digest
//
// # Error Caching
//
// - Verification failures: removed on next read, allows immediate retry
// - ErrNotSafelyDigestible: persists for full TTL; component used unverified
// - ErrResolutionInProgress: never cached; returned synchronously
//
// See ADR docs/adr/0009_controller_v2_lib_migration.md for details.
package resolution
