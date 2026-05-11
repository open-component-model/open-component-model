# Streaming Resource Content: Replacing Tar with oras Storage Abstraction

* **Status**: proposed
* **Deciders**: OCM Technical Steering Committee
* **Date**: 2026-05-11

Technical Story: Today, downloading a resource produces a tar file containing
the full OCI artifact. This causes out-of-memory crashes on large AI model
images (30+ GiB). We want to replace this with a streaming approach built on
oras-go's existing `content.ReadOnlyGraphStorage` interface.

## Context and Problem Statement

When you download a resource, the repository currently returns a single binary
blob (`blob.ReadOnlyBlob`). Since an OCI artifact is made up of many blobs
(one manifest, multiple layers), the system packs them all into a tar file to
fit the single-blob interface. This tar is written to a temp file before the
upload even starts.

Problems:

1. **Out-of-memory kills**: On systems where `/tmp` lives in RAM, a 30 GiB
   model image fills all available memory before upload begins.
2. **Wasted bandwidth**: Even if the destination already has most layers,
   everything gets downloaded first. No deduplication happens until after the
   full tar is built.
3. **Pointless round-trip**: Download packs blobs into tar. Upload unpacks tar
   back into blobs. The tar format adds nothing when both sides speak OCI.
4. **CTF also suffers**: CTF is just files on disk. It unpacks the tar right
   back into individual files — the tar step is pure overhead.

### What prompted this

An external contributor built a `TransferOCIArtifact` transformer that streams
directly between OCI registries. Their approach works but adds a `Transfer`
method to the repository and requires the graph builder to detect compatible
endpoints. We can do better by changing the underlying abstraction.

## Decision Drivers

* Stream content blob-by-blob — never hold the full artifact in memory or disk
* Reuse oras-go interfaces we already depend on instead of inventing new ones
* Keep backwards compatibility for transformers that need `blob.ReadOnlyBlob`
* No special-casing in the graph builder for specific backend pairs
* CTF and OCI targets both benefit equally

## Considered Options

1. **Fused Transfer Transformer** — single transformer node for OCI→OCI
2. **oras Storage as Resource Content** — return `content.ReadOnlyGraphStorage`
   instead of a blob
3. **Streaming Pipe** — connect Get and Add transformers with an io.Pipe

## Decision Outcome

Chosen Option 2: **oras Storage as Resource Content**.

We already depend on `oras.land/oras-go/v2`. Its interface hierarchy provides
exactly what we need: per-blob streaming, existence checks, and graph traversal.
Instead of inventing our own abstraction, we use what oras already defines.

## The Core Idea

oras-go defines these interfaces (simplified):

```go
// content.ReadOnlyStorage — can fetch individual blobs
type ReadOnlyStorage interface {
    Fetch(ctx context.Context, desc ocispec.Descriptor) (io.ReadCloser, error)
    Exists(ctx context.Context, desc ocispec.Descriptor) (bool, error)
}

// content.ReadOnlyGraphStorage — adds graph traversal
type ReadOnlyGraphStorage interface {
    ReadOnlyStorage
    Predecessors(ctx context.Context, node ocispec.Descriptor) ([]ocispec.Descriptor, error)
}

// content.Storage — can also push blobs
type Storage interface {
    ReadOnlyStorage
    Push(ctx context.Context, expected ocispec.Descriptor, content io.Reader) error
}
```

`oras.CopyGraph` already accepts these:

```go
func CopyGraph(ctx context.Context,
    src content.ReadOnlyStorage,
    dst content.Storage,
    root ocispec.Descriptor,
    opts CopyGraphOptions) error
```

It streams blob-by-blob, does HEAD checks to skip existing content, and never
creates a tar file. **We already use this internally.** The problem is that
our repository interface hides it behind a tar-producing `ReadOnlyBlob` return.

## Proposed Design

### OCI-specific streaming interface

The streaming API lives in the OCI package (`bindings/go/oci`), not in the
generic `repository.ResourceRepository` interface. It is specific to OCI-based
repositories because it exposes oras storage semantics that only make sense for
OCI content (manifests, layers, descriptors).

The generic `ResourceRepository` interface remains unchanged — it still uses
`blob.ReadOnlyBlob`. Repositories that support streaming expose the additional
interface alongside it:

```go
package oci

// StreamingResourceRepository extends the generic ResourceRepository with
// OCI-native streaming. Only implemented by OCI-backed repositories.
type StreamingResourceRepository interface {
    repository.ResourceRepository

    // DownloadResourceStream returns a store handle and root descriptor.
    // No data is downloaded yet — content streams on demand.
    DownloadResourceStream(ctx context.Context, resource *descriptor.Resource, credentials map[string]string) (ResourceStream, error)

    // UploadResourceStream writes content from a ResourceStream into this repository.
    UploadResourceStream(ctx context.Context, resource *descriptor.Resource, content ResourceStream, credentials map[string]string) (*descriptor.Resource, error)
}
```

`ResourceStream` wraps oras interfaces — also in the OCI package:

```go
package oci

// ResourceStream is a lazy handle to OCI content.
// It implements content.ReadOnlyGraphStorage so it can be passed
// directly to oras.CopyGraph or consumed blob-by-blob.
// This interface is OCI-specific — it only makes sense for content
// that consists of manifests and layers addressed by descriptor.
type ResourceStream interface {
    content.ReadOnlyGraphStorage

    // Root returns the top-level descriptor (manifest or index).
    Root() ocispec.Descriptor

    // Materialize produces a ReadOnlyBlob for legacy consumers.
    // This is the only path that creates a tar file.
    Materialize(ctx context.Context) (blob.ReadOnlyBlob, error)
}
```

`ResourceStream` **is** an oras store. Any code that knows how to work with
oras can use it directly. The generic `ResourceRepository` interface stays
untouched — callers that don't need streaming keep using `DownloadResource` /
`UploadResource` with `blob.ReadOnlyBlob` as before.

### How transfer works

The transfer logic checks if both source and target support streaming. If they
do, it uses the fast path. Otherwise it falls back to `DownloadResource` /
`UploadResource` with `blob.ReadOnlyBlob`:

```go
func transferResource(ctx context.Context, src, dst repository.ResourceRepository, res *descriptor.Resource, creds Credentials) error {
    // Try streaming path if both sides support it
    srcStream, srcOK := src.(oci.StreamingResourceRepository)
    dstStream, dstOK := dst.(oci.StreamingResourceRepository)

    if srcOK && dstOK {
        stream, err := srcStream.DownloadResourceStream(ctx, res, creds.Source)
        if err != nil {
            return err
        }
        _, err = dstStream.UploadResourceStream(ctx, res, stream, creds.Target)
        return err
    }

    // Fallback: legacy blob path
    blob, err := src.DownloadResource(ctx, res, creds.Source)
    if err != nil {
        return err
    }
    _, err = dst.UploadResource(ctx, res, blob, creds.Target)
    return err
}
```

Inside `UploadResourceStream`, the OCI repository implementation does:

```go
func (r *OCIRepository) UploadResourceStream(ctx context.Context, res *descriptor.Resource, content ResourceStream, creds map[string]string) (*descriptor.Resource, error) {
    dstStore := r.storeFor(creds)

    // Direct streaming — blob by blob, skips existing content
    err := oras.CopyGraph(ctx, content, dstStore, content.Root(), r.copyOpts)
    if err != nil {
        return nil, err
    }

    return r.tagAndDescribe(ctx, res, content.Root())
}
```

For CTF (filesystem) targets:

```go
func (c *CTFRepository) UploadResourceStream(ctx context.Context, res *descriptor.Resource, content ResourceStream, creds map[string]string) (*descriptor.Resource, error) {
    // CTF's internal store also implements content.Storage
    // oras.CopyGraph writes blobs as individual files — no tar
    err := oras.CopyGraph(ctx, content, c.layoutStore, content.Root(), oras.DefaultCopyGraphOptions)
    if err != nil {
        return nil, err
    }

    return c.updateIndex(ctx, res, content.Root())
}
```

Both paths use the same mechanism. No special cases. No tar.

### OCI implementation of ResourceStream

```go
type ociResourceStream struct {
    store content.ReadOnlyGraphStorage  // remote registry handle
    root  ocispec.Descriptor            // resolved manifest descriptor
}

func (c *ociResourceStream) Fetch(ctx context.Context, desc ocispec.Descriptor) (io.ReadCloser, error) {
    return c.store.Fetch(ctx, desc)
}

func (c *ociResourceStream) Exists(ctx context.Context, desc ocispec.Descriptor) (bool, error) {
    return c.store.Exists(ctx, desc)
}

func (c *ociResourceStream) Predecessors(ctx context.Context, node ocispec.Descriptor) ([]ocispec.Descriptor, error) {
    return c.store.Predecessors(ctx, node)
}

func (c *ociResourceStream) Root() ocispec.Descriptor {
    return c.root
}

func (c *ociResourceStream) Materialize(ctx context.Context) (blob.ReadOnlyBlob, error) {
    // Legacy fallback: build tar only when explicitly asked
    return tar.CopyToOCILayoutInMemory(ctx, c.store, c.root, nil)
}
```

`DownloadResourceStream` just resolves the reference — zero bytes downloaded:

```go
func (r *OCIRepository) DownloadResourceStream(ctx context.Context, res *descriptor.Resource, creds map[string]string) (ResourceStream, error) {
    store := r.storeFor(creds)
    ref := r.referenceFor(res)

    // Resolve tag/digest to descriptor — one HEAD request
    root, err := store.Resolve(ctx, ref)
    if err != nil {
        return nil, err
    }

    return &ociResourceStream{store: store, root: root}, nil
}
```

### Backwards compatibility for transformers

Transformers that need `blob.ReadOnlyBlob` call `Materialize()`:

```go
func (t *SomeTransformer) Transform(ctx context.Context, content ResourceStream) (blob.ReadOnlyBlob, error) {
    // Only this path creates a tar — and only when truly needed
    return content.Materialize(ctx)
}
```

Over time, transformers can be updated to work directly with `ResourceStream`
(which is just an oras store) and avoid tar entirely.

## Pros and Cons of the Options

### [Option 1] Fused Transfer Transformer

Combine Get+Add into one transformer for compatible backends.

Pros:

* Works now with minimal changes
* Existing interfaces stay the same

Cons:

* Adds `Transfer` method to repository public API
* Graph builder must detect compatible endpoint pairs
* Need a new fused transformer for each backend pair
* CTF still gets tar
* Transformers gain repository access (breaks their contract)

### [Option 2] oras Storage as Resource Content

Return an oras `content.ReadOnlyGraphStorage` from DownloadResourceStream.

Pros:

* Built on oras interfaces we already use — no new abstractions
* `oras.CopyGraph` does all the heavy lifting (streaming, dedup, HEAD checks)
* Works identically for OCI→OCI, OCI→CTF, any→any
* Graph builder needs zero special cases
* `Materialize()` keeps full backwards compatibility
* One line of transfer code regardless of backends

Cons:

* Breaking change to `ResourceRepository` interface
* All repository implementations need updating
* `BlobTransformer` consumers need to call `Materialize()` (trivial adapter)
* Slightly larger interface surface than a single `ReadOnlyBlob`

### [Option 3] Streaming Pipe

Connect Get and Add with an io.Pipe for streaming.

Pros:

* No interface changes
* Bounded memory usage

Cons:

* Requires concurrent graph node execution
* Still serializes full artifact through the pipe
* No per-blob dedup — downloads everything regardless
* CTF doesn't benefit
* Backpressure adds complexity

## Migration Path

| Phase | What changes | Risk |
|-------|-------------|------|
| 1 | Add `DownloadResourceStream` returning `ResourceStream` alongside existing `DownloadResource` in OCI repo | None — additive |
| 2 | Transfer graph calls `DownloadResourceStream` + `UploadResourceStream`. Transformers use `Materialize()` | Low — same behavior via Materialize |
| 3 | CTF `UploadResourceStream` uses `oras.CopyGraph` directly against content store | Medium — new write path |
| 4 | Deprecate `DownloadResource`. Remove tar from default paths | Cleanup only |

## Interaction with Existing ADRs

* **ADR 0003 (Transfer)**: Transfer spec and graph structure unchanged.
  Optimization is inside `UploadResourceStream`, transparent to the graph.
* **ADR 0005 (Transformation)**: Transformers call `Materialize()` when they
  need bytes. Future transformers can accept `ResourceStream` directly.
* **ADR 0007 (BlobTransformer)**: `BlobTransformer` still works on
  `blob.ReadOnlyBlob`. Bridge: `content.Materialize(ctx)`.

## Conclusion

The tar file was never a deliberate design choice — it was a consequence of
returning a single `ReadOnlyBlob` for multi-blob OCI content. By returning an
oras `content.ReadOnlyGraphStorage` instead, we let `oras.CopyGraph` do what it
already does: stream blobs one at a time, skip what the destination has, and
never buffer the full artifact. No new abstractions needed — just exposing the
oras store that already exists inside our repository implementations.
