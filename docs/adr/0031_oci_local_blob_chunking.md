# Chunk Oversized OCI Local Blobs

* **Status**: accepted
* **Deciders**: Fabian Burth (@fabianburth), Jakob Möller (@jakobmoellerdev)
* **Date**: 2026-09-23

## Context and Problem Statement

OCM must transport opaque artifacts such as virtual machine disk images that exceed the per-request body limits many OCI registries enforce on blob uploads. The component descriptor must continue to describe one logical resource; how the bytes reach the registry is a transport detail and must not leak into the model.

Two concrete problems block this today:

* A monolithic blob upload sends the whole blob in a single request. A registry that caps the request body rejects an oversized blob outright, even when its total size is within the registry's blob-size policy.
* `oci/internal/pack.ResourceLocalBlobOCILayer` calls `PrepareArtifactBlobForOCI`, which invokes `ArtifactBlob.Buffer()` when the blob's size or digest is unknown. The OCI `content.Storage.Push` contract requires a complete descriptor (digest and size) before upload, so an unknown-descriptor blob is fully materialized in memory purely to precompute that descriptor.

The solution must not change how content is represented at rest. Splitting an opaque blob into multiple stored layers, or wrapping it in a new manifest, would change the stored graph, the reader contract, and the set of clients that can read the content — none of which is required to get bytes past a request-body limit.

## Decision Drivers

* Keep resource identity, digest, media type, and signatures independent of how the upload is transported.
* Respect registry request-body limits without buffering the complete resource in memory.
* Preserve OCI CAS deduplication: one blob remains one content-addressable blob with one digest.
* Require no new on-registry format and no chunk-aware reader; any OCI client can still fetch the blob.
* Keep chunking outside input methods, storage layout, and the component descriptor schema.
* Support blobs whose size and digest are unknown until the bytes have been read once.

## Decision Outcome

Chosen option: **transport-level chunked blob upload per the OCI Distribution Spec**, implemented on `remotestore.RemoteStore`.

An oversized opaque blob is uploaded to the registry using the [OCI Distribution Spec chunked blob upload](https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md#pushing-a-blob-in-chunks): open an upload session (`POST`), stream the content in bounded `PATCH` requests carrying `Content-Range`, then close the session (`PUT ?digest=...`) with the whole-blob digest. The registry reassembles the single blob; on the wire and at rest it is one ordinary content-addressable blob with one digest — identical to a monolithic upload. Only the request framing differs.

Because the whole-blob digest is only needed to *close* the session, the same mechanism also enables streaming: `PushStreaming` uploads content whose digest and size are unknown up front, computing and returning both from the streamed bytes. The `pack` layer streams unknown-descriptor resource blobs through this path instead of buffering them into memory to precompute a descriptor.

This design needs no new manifest, no new media type, no chunk index, and no chunk-aware reader. Reads are unchanged: a standard OCI fetch returns the reassembled blob.

### Rejected representation-level alternatives

The following alternatives were considered and rejected because transport-level chunking solves the size problem without any of their cost. They are recorded to explain why a new on-registry representation was deliberately avoided.

#### Alternative A: OCI manifest with ordered chunk layers

Point `LocalBlob.localReference` at an OCI image manifest whose ordered `layers` are byte chunks, with a config blob describing the logical content.

* Pro: OCI manifests already model an ordered list of blobs; one graph root; standard traversal.
* Con: introduces a new wire contract and artifact type; standard OCI clients can copy but not reconstruct the logical blob; requires a chunk-aware reader to concatenate layers; suppresses `globalAccess` because the reference no longer yields the logical byte stream; adds config and manifest fetches. All of this to work around a request-body limit that the distribution spec already addresses at the transport layer.

#### Alternative B: OCM split-descriptor blob

Point `localReference` at a custom JSON blob listing chunks, with custom graph traversal and reachability handling.

* Con: duplicates OCI manifest functionality; not traversable by OCI tooling or registry garbage collection; requires bespoke reachability and transfer logic.

#### Alternative C: digest list in `localReference`

Encode ordered chunk digests in the reference string, as OCM v1 did with comma-separated values.

* Con: overloads `localReference` syntax and semantics; lacks sizes, whole-content metadata, and a versioned format; every reader must parse a non-reference value; chunks are not a coherent OCI graph.

#### Alternative D: OCI image index of chunks

Use an OCI image index as the chunk root.

* Con: an index models a set of manifests (commonly platform variants), not an ordered byte sequence; each chunk would need a manifest wrapper or would abuse index semantics; unnecessary graph depth for consumers.

## Protocol

`RemoteStore.Push` selects the upload strategy from the descriptor and configuration; the returned bytes at rest are identical regardless of the path taken.

* Manifests, blobs below the effective chunk threshold, and blobs that fit in a single chunk take the embedded monolithic push (`oras remote.Repository`, which has no chunked path).
* Blobs at or above the effective threshold — the maximum of `ChunkThreshold` and `ChunkSize`, so a lone `PATCH` plus closing `PUT` is never chosen over a plain monolithic upload — take the chunked path.

The chunked path runs three phases:

1. **Open** — `POST` a blob upload session; the response `Location` is the first push target.
2. **Upload** — `PATCH` bounded chunks with `Content-Range: <start>-<end>`, advancing the location returned by each response; a running digester hashes the streamed bytes.
3. **Close** — `PUT ?digest=<whole-blob-digest>` finalizes the blob.

Correctness and security constraints:

* The registry-advertised `OCI-Chunk-Min-Length` raises the effective chunk size when it exceeds the configured one.
* `MaxChunkSize` (128 MiB) bounds the `PATCH` buffer; a configured `ChunkSize` or an advertised `OCI-Chunk-Min-Length` above it is rejected rather than allowed to exhaust memory.
* Each `Location` host and scheme is validated before use to avoid leaking credentials to a redirected host (the same check oras applies).
* When a known digest or size is supplied, the streamed content is verified against it before the session is closed; a mismatch cancels the session and returns an error.

### Failure handling

* **Pre-consumption failure** (session `POST` fails, or the first `PATCH` has not been sent): the regular `Push` falls back to the embedded monolithic push, so no registry regresses. `PushStreaming` has no monolithic fallback — a monolithic upload needs `Content-Length` up front — so it returns an error wrapping `ErrStreamingUnavailable` without consuming content, letting the caller buffer and retry via `Push`.
* **Post-consumption failure** (a chunk has already been sent; an `io.Reader` cannot be rewound): the upload returns a wrapped error and best-effort `DELETE`s the session.

### Streaming unknown-descriptor blobs

`PushStreaming` uploads content described only by a partial descriptor:

* a non-empty `Digest` is treated as expected and verified against the streamed bytes; an empty `Digest` is computed from them;
* a non-negative, non-zero `Size` is verified against the number of bytes streamed; a zero or absent size is treated as unknown (an empty blob uploads as a single empty chunk verified by digest);
* the returned descriptor carries the final digest and size.

In the `pack` layer, `ResourceLocalBlobOCILayer` streams the resource blob through `PushStreaming` whenever the storage implements `StreamingPusher` and the blob would otherwise be buffered — that is, when its size *or* digest is unknown. A blob that already exposes both size and digest needs no buffer and takes the regular `Push` path, which still chunks it by size when large. This removes the `ArtifactBlob.Buffer()` / cache round-trip that previously materialized unknown-descriptor blobs in memory solely to precompute the descriptor.

## Configuration

Chunked upload is a property of the remote store, wired through the resolver:

```go
urlresolver.WithChunkedPush(chunkSize, threshold)
```

* `chunkSize` is the target `PATCH` chunk size in bytes; `chunkSize <= 0` disables chunking (and therefore streaming).
* `threshold` is the minimum blob size for chunking to engage; `threshold <= 0` uses `DefaultChunkThreshold`.
* Defaults are 16 MiB for both `DefaultChunkSize` and `DefaultChunkThreshold`.
* Chunked upload is bypassed when a blob or reference cache is active on the resolver, because those wrap the raw `*remote.Repository`; the store handed out is then the cache proxy, not the chunk-capable `RemoteStore`.

The setting applies to opaque local blobs uploaded to remote OCI repositories. Native OCI artifacts and OCI image layouts are unaffected: their manifests, layers, and digests are untouched. CTF and other non-remote stores keep their existing push behavior; the chunked path is specific to the registry transport.

## Compatibility

* The feature changes only upload request framing. The stored blob, its digest, its media type, and its reachability are identical to a monolithic upload, so any OCI client — including old OCM clients — reads chunk-uploaded content transparently.
* No component descriptor, `LocalBlob`, or storage-layout change is required.
* A registry that does not support chunked upload triggers the pre-consumption fallback to monolithic push (for `Push`) or `ErrStreamingUnavailable` (for `PushStreaming`).

## Validation and Test Requirements

Cover at least:

* the exact `POST` / `PATCH` / `PUT` sequence and `Content-Range` headers;
* `ChunkSize=0`, sub-threshold blobs, and manifests staying monolithic (no `PATCH`);
* `OCI-Chunk-Min-Length` raising the effective chunk size;
* pre-consumption `POST` rejection falling back to monolithic push;
* post-consumption `PATCH` rejection returning an error (no silent success) and cancelling the session;
* streaming computing the digest and size from the stream, and verifying a supplied digest/size;
* streaming-unavailable cases returning `ErrStreamingUnavailable` without consuming content;
* a round-trip against a real registry for both known-digest `Push` and unknown-digest `PushStreaming`;
* proof that `PushStreaming` does not buffer: an interleaving reader must observe a `PATCH` reach the registry before the whole blob is read.

## Consequences

* Large opaque local blobs fit registries with request-body limits without changing logical resource identity or the stored representation.
* Unknown-descriptor resource blobs stream to the registry instead of buffering in memory, bounding RAM to the chunk buffer.
* The upload is sequential; concurrent chunk upload and resumable sessions are possible future work but are not implemented.
* A registry without chunked-upload support silently falls back to monolithic push and remains subject to its request-body limit.
* Content-addressable deduplication is preserved because one blob remains one blob with one digest.
