# Deletion of OCM Component Versions in OCI Repositories

* **Status**: proposed
* **Deciders**: OCM Technical Steering Committee
* **Date**: 2026-07-24

**Technical Story**: The OCI binding (`bindings/go/oci`) can add, get, and list OCM component versions, and it can untag floating aliases, but it cannot delete a component version. Users need a supported way to remove a component version — its descriptor, manifest/index, and the local blobs it owns — from an OCI-backed repository (both remote registries and CTF archives), without corrupting other versions that share content.

---

## Context and Problem Statement

An OCM component version stored via `bindings/go/oci` is a graph of OCI content:

- A **top-level artifact** tagged with the version: either an image manifest or, when the version carries additional local artifact manifests, an image index (see `AddDescriptorToStore`).
- A **component config** blob (`application/vnd.ocm.software.component.config.v1+json`) referencing the descriptor layer.
- A **descriptor layer** blob (the serialized component descriptor).
- Zero or more **local resource/source blobs and nested manifests**, referenced from the descriptor through `LocalBlob.LocalReference` and carried as manifest layers or index entries.

Today the repository interface (`repository.ComponentVersionRepository`) has no delete operation. The only removal primitive is `RemoveComponentVersionAlias` (if supported), which uses `content.Untagger` and deliberately removes **only a tag pointer**, never content.

Deleting a component version is harder than untagging because:

1. **Content is deduplicated by digest.** Local blobs are content-addressed. The same layer (e.g. a base image or a shared config) can be referenced by multiple component versions in the same repository. Blindly deleting a version's reachable blobs can break other versions.
2. **Backends have different deletion capabilities.**
   - Remote registries treat `DELETE /v2/<name>/manifests/<digest|tag>` as **optional**; many disable it (`405 Method Not Allowed`, surfaced today as `remotestore.ErrTagDeletionDisabled`). Registries that support delete run their **own asynchronous garbage collection** for unreferenced blobs.
   - CTF archives support `DeleteBlob`, `Untag`, and `RemoveTag` on the index, but perform **no automatic garbage collection** — orphaned blobs stay on disk forever unless explicitly removed.
3. **The `spec.Store` contract does not include deletion.** It is `content.ReadOnlyGraphStorage + content.Pusher + content.TagResolver`. Any delete path must negotiate optional capabilities (`content.Deleter`, `content.Untagger`) at runtime and fail cleanly when they are absent.

We need a deletion design that is safe (never breaks a surviving version), portable (works against both backends and degrades cleanly), and honest about what it reclaims on each backend.

---

## Decision Drivers

* Deleting one component version MUST NOT corrupt or orphan any other version in the same repository.
* Must work for both backends: remote OCI registries and CTF archives.
* Must degrade cleanly when a backend forbids deletion (GC-disabled registry, read-only store) with a typed, checkable error.
* Must keep `ListComponentVersions` consistent.
* Must be an optional capability, consistent with the existing pattern (`HealthCheckable`, `ComponentVersionDeleter`, `content.Untagger`), because not every store can delete.
* Must not silently leave disk garbage on CTF, where nothing else will ever collect it.
* Should reuse existing primitives (`content.Untagger`, `content.Deleter`, CTF index `RemoveTag`/`DeleteBlob`) rather than inventing new store contracts where avoidable.

---

## Considered Options

* **Option 1 — Untag-only (soft delete).** Remove the version tag. Leave all content in place for backend GC to reclaim eventually.
* **Option 2 — Logical delete + backend-delegated GC (with explicit CTF prune).** Untag, delete the top-level manifest/index by digest, then reclaim blobs via the backend's own GC (registries) or an explicit reachability-based prune (CTF).
* **Option 3 — Eager recursive delete with cross-version reference counting.** On every delete, walk the full version graph, compute which blobs are referenced by any other surviving version, and delete exactly the orphans synchronously across all backends.

---

## Decision Outcome

Chosen [Option 2](#option-2-logical-delete--backend-delegated-gc): "Logical delete with backend-delegated garbage collection and an explicit CTF prune".

Justification:

* It is **safe by construction**: the phase that could break other versions (physical blob removal) is a separate reachability-checked step, and on registries it is delegated to the registry's proven GC.
* It is **portable and honest**: the same call works on both backends and reports what it did. GC-disabled registries and read-only stores fail with a typed error instead of pretending to delete.
* It avoids the expensive, race-prone full graph reference-counting of Option 3 in the common (registry) case, while still giving CTF users a deterministic way to reclaim space.

### Option 2 — Logical delete + backend-delegated GC

#### Description

Deletion is modelled as an optional repository capability with two conceptual phases:

1. **Logical delete (portable, always attempted).**
   Remove the reachability roots for the version so it can no longer be resolved or listed:
   - Resolve the version tag to the top-level descriptor.
   - Validate it is an OCM component version for the requested component (by decoding and unmarshaling it as a valid descriptor) to avoid deleting an unrelated tag.
   - `Untag` the version (via `content.Untagger`).
   - Delete the top-level manifest/index by digest (via `content.Deleter`), if the store supports it.

2. **Blob reclamation (backend-specific).**
   - **Remote registries**: stop after deleting the manifest. The registry's own garbage collector reclaims now-unreferenced blobs asynchronously.
   - **CTF archives**: run an explicit, reachability-based prune. Because CTF is a closed world (the whole index is enumerable and locked), we can safely compute the set of blobs reachable from all *surviving* tagged artifacts and delete every blob not in that set using `DeleteBlob`.

The operation is **idempotent**: deleting an already-absent version returns `repository.ErrNotFound` (joined with the backend error).

#### Contract

A new optional capability interface, mirroring the existing `AliasComponentVersionRepository` and `OwnershipAwareRepository` patterns:

```go
// ComponentVersionDeleter is an optional capability of a
// ComponentVersionRepository. It removes a component version and the
// content it exclusively owns from the underlying store.
//
// Implementations MUST NOT remove content that is still referenced by another
// surviving component version in the same store. Reclamation of unreferenced
// blobs MAY be delegated to the backend's own garbage collector (e.g. an OCI
// registry) or performed explicitly (e.g. a CTF archive prune).
type ComponentVersionDeleter interface {
	// DeleteComponentVersion removes the given component version from the
	// repository. It untags the version, drops any referrer bookkeeping, and
	// removes the version's top-level manifest/index. Blobs owned exclusively by
	// this version are reclaimed according to the backend's capabilities.
	//
	// Returns ErrNotFound (joined with the backend error) if the
	// version does not exist. Returns ErrDeleteUnsupported if the backing store
	// cannot delete (e.g. a registry with tag/manifest deletion disabled, or a
	// read-only store).
	DeleteComponentVersion(ctx context.Context, component, version string) error
}

// ErrDeleteUnsupported indicates the backing store does not permit deletion.
var ErrDeleteUnsupported = errors.New("component version deletion not supported by store")
```

Store-level requirements (negotiated at runtime, no change to `spec.Store`):

- `Untag` requires `content.Untagger`. `RemoteStore` and CTF both implement it now in this branch.
- Manifest/index removal requires `content.Deleter`.
- The CTF prune uses the existing archive APIs under the repository index lock, consistent with other index-mutating operations.

---

## Pros and Cons of the Options

### Option 1 — Untag-only (soft delete)

Pros:

* Trivial to implement; reuses `content.Untagger`.
* Fully portable; never risks breaking a shared blob.

Cons:

* Reclaims nothing on CTF — orphaned descriptor/config/blobs accumulate on disk forever.
* On registries the manifest lingers; untag alone often does not trigger blob GC.
* Users reasonably expect "delete" to reclaim space, not just hide a tag.

### Option 2 — Logical delete + backend-delegated GC (with explicit CTF prune)

Pros:

* Safe: the space-reclaiming phase is reachability-checked (CTF) or delegated to a proven collector (registries).
* Portable and honest across both backends, with a typed `ErrDeleteUnsupported` for GC-disabled/read-only stores.
* Reuses existing primitives; no change to the `spec.Store` contract.

Cons:

* Two code paths (registry vs CTF) to maintain and test.
* On registries, reclamation is asynchronous and not directly observable from OCM.

### Option 3 — Eager recursive delete with cross-version reference counting

Pros:

* Deterministic, immediate space reclamation on all backends.
* Single, uniform algorithm regardless of backend.

Cons:

* Requires walking all other component versions on every delete — expensive and racy.
* High risk: a reference-counting bug deletes a blob still used by a surviving version.

## Discovery and Distribution

* Implemented in `bindings/go/oci` on `*Repository` as `DeleteComponentVersion`, gated behind the new `ComponentVersionDeleter` interface in `bindings/go/repository`.
* Callers detect support via a type assertion (`repo.(repository.ComponentVersionDeleter)`), consistent with `HealthCheckable`.
* Errors are typed: `repository.ErrNotFound` for missing versions, `ErrDeleteUnsupported` for stores that forbid deletion.
* Tests: testify unit tests for CTF-backed deletion, shared-blob survival, and list consistency; integration tests using Testcontainers and a real Registry V3 instance verify the remote OCI registry path.

## Conclusion

OCM will support deleting OCM component versions from OCI repositories through an optional `ComponentVersionDeleter` capability. Deletion untags the version and deletes the top-level manifest/index; blob reclamation is delegated to the registry's garbage collector for remote backends and performed by an explicit reachability-based prune for CTF archives. This keeps deletion safe for shared content, portable across backends, and transparent about what it reclaims, while degrading cleanly on stores that forbid deletion.
