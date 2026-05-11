# Streaming Resource Content: Eliminating Tar Materialization in Transfer

* **Status**: proposed
* **Deciders**: Fabian Burth, Jakob Moeller
* **Date**: 2026-05-11

Technical Story: Replace the `blob.ReadOnlyBlob` return type in the resource
repository download path with a lazy `ResourceContent` handle that allows
direct streaming between compatible backends, eliminating the intermediate
tar materialization that causes OOM kills on large (30+ GiB) AI model images.

## Context and Problem Statement

The current `ResourceRepository.DownloadResource()` returns a
`blob.ReadOnlyBlob` — a fully materialized binary blob. Because OCI artifacts
consist of multiple blobs (manifest + N layers), the only way to pack them into
a single `ReadOnlyBlob` is to serialize them into a tar archive (OCI Layout
format). This tar is written to a temporary file (or held in memory on
tmpfs-backed `/tmp`), creating several problems:

1. **OOM on large artifacts**: Hosts with tmpfs-backed `/tmp` materialize
   multi-GiB images entirely in RAM before upload begins. 30+ GiB model images
   cause silent OOM kills.
2. **Redundant downloads**: Even when the destination already has every blob
   (e.g. re-transfer), the source is fully downloaded and serialized before
   any HEAD check occurs.
3. **Unnecessary serialization round-trip**: Download serializes to tar, upload
   deserializes from tar — for no reason when both ends are OCI registries.
4. **CTF targets also suffer**: Writing to a CTF (OCI Layout on disk) unpacks
   the tar back into individual files — the tar step adds pure overhead.

An external contributor independently implemented a `TransferOCIArtifact`
transformer that fuses the Get+Add pair into a single direct-streaming node
(see `fix/oci-direct-streaming-transfer`). While effective, it introduces a
`Transfer` method on the repository and requires special-casing in the graph
builder. This ADR proposes a cleaner abstraction at the repository interface
level.

## Decision Drivers

* Zero-copy transfer between compatible backends (OCI→OCI, S3→S3)
* No new public API surface on `ResourceRepository` beyond the existing contract
* Backwards-compatible: existing consumers of `blob.ReadOnlyBlob` continue to work
* Transformers must not need repository access for transfer orchestration
* CTF and other filesystem targets benefit equally (no tar round-trip)
* Extensible to future backend-to-backend optimizations without new transformer types

## Considered Options

1. **[Option 1](#option-1-fused-transfer-transformer)**: Fused Transfer
   Transformer (external branch approach)
2. **[Option 2](#option-2-lazy-resourcecontent-with-direct-transfer-negotiation)**:
   Lazy ResourceContent with Direct Transfer Negotiation
3. **[Option 3](#option-3-streaming-pipe-between-get-and-add)**: Streaming
   Pipe between Get and Add transformers

## Decision Outcome

Chosen [Option 2](#option-2-lazy-resourcecontent-with-direct-transfer-negotiation):
"Lazy ResourceContent with Direct Transfer Negotiation".

Justification:

* Eliminates tar at the interface level, not just for OCI→OCI
* No new public methods on repository — optimization is internal to the content handle
* Graph builder needs no special-casing — same graph works for all targets
* Extensible: any backend pair can implement `DirectTransferable` independently
* CTF targets benefit immediately by consuming layers individually

### Option 2

#### Description

Replace `blob.ReadOnlyBlob` as the return type of resource download operations
with a new `ResourceContent` interface. This interface provides lazy access to
the underlying data: individual layers can be streamed, the content can be
materialized to a blob on demand, or — when source and target are compatible —
transferred directly without any local I/O.

#### High-level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      Transfer Graph                              │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  GetResource(src)          PutResource(dst, content)            │
│       │                          ▲                              │
│       ▼                          │                              │
│  ResourceContent ────────────────┘                              │
│       │                                                         │
│       ├── TransferTo(dst) ──→ direct copy (oras.CopyGraph)      │
│       │                       [OCI→OCI: zero local I/O]         │
│       │                                                         │
│       ├── Layers() ──→ individual blob streaming                │
│       │                 [CTF: write blobs directly to dir]       │
│       │                                                         │
│       └── Materialize() ──→ blob.ReadOnlyBlob (tar)             │
│                              [legacy fallback only]              │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

#### Contract

```go
package repository

import (
    "context"
    "io"

    "ocm.software/open-component-model/bindings/go/blob"
    ocispec "github.com/opencontainers/image-spec/specs-go/v1"
    "github.com/opencontainers/go-digest"
)

// ResourceContent is a lazy handle to resource data returned by GetResource.
// No I/O occurs until a consumption method is called.
type ResourceContent interface {
    // Descriptor returns the root OCI descriptor for the content.
    Descriptor() ocispec.Descriptor

    // Layers returns handles to individual content blobs.
    // Each layer can be streamed independently without materializing the whole.
    Layers(ctx context.Context) ([]LayerContent, error)

    // Materialize serializes the full content into a ReadOnlyBlob.
    // This is the legacy path — only called when no better consumption method
    // is available. Implementations MAY produce a tar-based OCI layout here.
    Materialize(ctx context.Context) (blob.ReadOnlyBlob, error)
}

// LayerContent provides streaming access to a single blob/layer.
type LayerContent interface {
    Digest() digest.Digest
    Size() int64
    MediaType() string
    Open(ctx context.Context) (io.ReadCloser, error)
}

// DirectTransferable is optionally implemented by ResourceContent when the
// source can push content directly to a compatible target repository without
// local materialization.
type DirectTransferable interface {
    // TransferTo performs a direct backend-to-backend transfer.
    // Returns ErrNotSupported if the target is not compatible.
    TransferTo(ctx context.Context, target ResourceRepository, creds map[string]string) error
}
```

Updated repository interface:

```go
type ResourceRepository interface {
    // GetResource returns a lazy content handle. No download occurs yet.
    GetResource(ctx context.Context, resource *descriptor.Resource, credentials map[string]string) (ResourceContent, error)

    // PutResource accepts a ResourceContent and writes it to the repository.
    // Implementations SHOULD consume via Layers() when possible,
    // falling back to Materialize() only when necessary.
    PutResource(ctx context.Context, resource *descriptor.Resource, content ResourceContent, credentials map[string]string) (*descriptor.Resource, error)

    // PutBlob preserves backwards compatibility for pre-materialized blobs.
    PutBlob(ctx context.Context, resource *descriptor.Resource, data blob.ReadOnlyBlob, credentials map[string]string) (*descriptor.Resource, error)
}
```

Transfer orchestration:

```go
func transferResource(ctx context.Context, src, dst ResourceRepository, res *descriptor.Resource, creds Credentials) error {
    content, err := src.GetResource(ctx, res, creds.Source)
    if err != nil {
        return err
    }

    // Fast path: direct backend-to-backend transfer
    if dt, ok := content.(DirectTransferable); ok {
        err := dt.TransferTo(ctx, dst, creds.Target)
        if err == nil {
            return nil
        }
        if !errors.Is(err, ErrNotSupported) {
            return err
        }
        // Fall through to standard path
    }

    // Standard path: let target consume content optimally
    return dst.PutResource(ctx, res, content, creds.Target)
}
```

## Pros and Cons of the Options

### [Option 1] Fused Transfer Transformer

The approach from `fix/oci-direct-streaming-transfer`: add a
`TransferOCIArtifact` transformer that combines Get+Add into a single graph
node when both endpoints are OCI registries.

Pros:

* Works today — minimal changes to existing interfaces
* Isolated to transformer layer — repository interface untouched
* Graph builder can optimize node count

Cons:

* Exposes `Transfer` method as public API on repository
* Graph builder needs special-case detection for compatible endpoints
* Each new backend pair (S3→S3, GCS→GCS) requires a new fused transformer
* CTF targets don't benefit — still get tar path
* Transformers need repository access, violating their current contract
* Type checks not clean — runtime interface assertion required

### [Option 2] Lazy ResourceContent with Direct Transfer Negotiation

Replace `ReadOnlyBlob` return with lazy `ResourceContent` handle.

Pros:

* Tar eliminated at the interface level for all paths
* No new public API on repository — optimization lives in content handle
* Same transfer graph works for all target types
* CTF can consume layers directly without tar round-trip
* Extensible: any backend pair implements `DirectTransferable` independently
* `Materialize()` provides clean backwards compatibility
* Blob dedup (HEAD checks) happens naturally via `oras.CopyGraph`

Cons:

* Breaking change to `ResourceRepository` interface (mitigated by `PutBlob` compat)
* All repository implementations must be updated
* `BlobTransformer` (ADR 0007) operates on `blob.ReadOnlyBlob` — needs adapter
* More complex interface than single `ReadOnlyBlob`

### [Option 3] Streaming Pipe between Get and Add Transformers

Keep the two-node graph (Get + Add) but replace the File spec intermediary
with an in-process pipe/channel that streams data between them.

Pros:

* No interface changes — only internal transformer changes
* Memory bounded by pipe buffer size
* Works within existing graph execution model

Cons:

* Requires concurrent execution of Get and Add nodes (graph must support it)
* Still serializes/deserializes OCI format through the pipe
* No blob-level dedup — entire artifact streams regardless of what target has
* Doesn't help CTF — still receives serialized stream
* Pipe backpressure complexity

## Interaction with Existing ADRs

### ADR 0003 (Transfer Specification)

The transfer specification and transformation pipeline remain unchanged. The
`ResourceContent` handle is consumed by transformers via `Materialize()` when
they need `blob.ReadOnlyBlob` semantics. The direct transfer path is
transparent to the graph — it's an optimization within `PutResource`.

### ADR 0005 (Transformation)

Transformers that operate on `blob.ReadOnlyBlob` (e.g. localization,
format conversion) continue to work by calling `content.Materialize()`.
Future transformers may accept `ResourceContent` directly for streaming
transformations.

### ADR 0007 (Resource Download / BlobTransformer)

The `BlobTransformer` interface operates on `blob.ReadOnlyBlob`. An adapter
bridges `ResourceContent` to `BlobTransformer`:

```go
func adaptForTransformer(ctx context.Context, content ResourceContent) (blob.ReadOnlyBlob, error) {
    return content.Materialize(ctx)
}
```

This preserves full compatibility while allowing the download path to defer
materialization until a transformer actually needs it.

## Migration Path

1. **Phase 1**: Introduce `ResourceContent` interface alongside existing
   `DownloadResource`. Add `GetResource` as new method returning
   `ResourceContent`. Implement for OCI repository.
2. **Phase 2**: Update transfer graph to use `GetResource` + `PutResource`.
   Existing transformers use `Materialize()` adapter.
3. **Phase 3**: Update CTF repository to consume `Layers()` directly.
   Deprecate `DownloadResource`.
4. **Phase 4**: Remove tar intermediary from all standard paths.
   `Materialize()` becomes legacy-only fallback.

## Discovery and Distribution

* Implementation starts in `bindings/go/repository` with new interface types
* OCI repository (`bindings/go/oci`) implements `ResourceContent` + `DirectTransferable`
* CTF repository implements `PutResource` consuming `Layers()` directly
* Transfer graph updated to call negotiation logic
* Existing tests adapted incrementally per phase

## Conclusion

By replacing `blob.ReadOnlyBlob` with a lazy `ResourceContent` handle at the
repository interface level, tar materialization is eliminated as an
architectural constraint rather than worked around per-backend. The
`DirectTransferable` negotiation pattern provides zero-copy transfer between
compatible backends without polluting the repository API or requiring
graph builder special-cases. The approach is backwards-compatible via
`Materialize()` and incrementally adoptable across repository implementations.
