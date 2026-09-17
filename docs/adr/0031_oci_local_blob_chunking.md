# Chunk Oversized OCI Local Blobs

* **Status**: proposed
* **Deciders**: Fabian Burth (@fabianburth)
* **Date**: 2026-09-16

## Context and Problem Statement

OCM must transport opaque artifacts such as VM disk images that exceed OCI registry per-blob limits. The component descriptor must continue to describe one logical resource; chunking is a repository representation detail.

Current paths do not satisfy this:

* `oci/internal/pack.ResourceLocalBlobOCILayer` stores an opaque local blob as one OCI layer.
* `PrepareArtifactBlobForOCI` calls `ArtifactBlob.Buffer()` when size or digest is unknown; that cache is fully in-memory.
* nested OCI artifacts are materialized as gzipped OCI-layout tar streams; the tar writer stages the complete layout in a temporary file.
* local-resource transfer writes the complete result of `GetLocalResource` to a temporary file before `AddLocalResource` reads it again.
* CTF directory storage is already an OCI content graph. CTF TAR/TGZ extraction and creation remain whole-archive operations by definition.

The solution must not split layers of native OCI artifacts. Rewriting such layers changes their manifests and digests and can make them unusable by OCI clients.

## Decision Drivers

* Keep resource identity, digest, media type, and signatures independent of chunk size.
* Respect registry blob limits without buffering the complete resource.
* Preserve OCI CAS deduplication and direct graph copy where possible.
* Use the same graph representation in OCI registries and directory-backed CTFs.
* Keep chunking outside input methods and the component descriptor schema.
* Transparently return the original byte stream to OCM consumers.
* Remain compatible with existing unsplit content.

## Considered Representations

### Option 1: OCI manifest with ordered chunk layers

`LocalBlob.localReference` points to an OCI image manifest. Its ordered `layers` are byte chunks. A small config blob describes the complete logical content.

Structural changes:

* add chunk-manifest packing and reading under `bindings/go/oci`;
* teach component-version storage to retain the manifest and its graph;
* keep `LocalBlob/v1` unchanged;
* add a repository/provider chunk-size option.

Pros:

* OCI manifests already model an ordered list of blobs;
* one graph root; standard `oras.CopyGraph` traversal;
* no `localReference` schema change;
* direct storage in OCI and CTF;
* self-describing and independently verifiable graph.

Cons:

* old clients do not understand the chunk artifact;
* standard OCI clients can copy, but not reconstruct, the logical blob;
* one extra config and manifest fetch.

### Option 2: OCM split-descriptor blob

`localReference` points to a custom JSON blob listing chunks.

Structural changes:

* define an OCM descriptor and custom graph traversal;
* explicitly retain otherwise-unreachable child blobs;
* implement copy logic outside normal OCI manifest traversal.

Pros:

* storage-neutral concept;
* single-valued `localReference`.

Cons:

* duplicates OCI manifest functionality;
* not naturally traversable by OCI tooling or registry garbage collection;
* requires custom reachability and transfer handling.

### Option 3: Digest list in `localReference`

Encode ordered chunk digests in the existing string, as OCM v1 did with comma-separated values.

Pros:

* no extra metadata object;
* proven basic streaming behavior in OCM v1.

Cons:

* overloads `localReference` syntax and semantics;
* lacks sizes, whole-content metadata, and a versioned format;
* every reader must parse a non-reference value;
* chunks are not an OCI graph.

### Option 4: OCI image index of chunks

Use an OCI image index as the chunk root.

Pros:

* OCI-native graph root;
* resembles the component-version index path.

Cons:

* an index models a set of manifests, commonly platform variants, not an ordered byte sequence;
* each chunk would need another manifest wrapper, or raw blobs would be used contrary to index semantics;
* unnecessary graph depth and ambiguity for OCI consumers.

## Decision Outcome

Chosen Option: **[Option 1](#option-1-oci-manifest-with-ordered-chunk-layers)**.

A single manifest matches the required ordered-blob model directly. It supports OCI graph traversal, registry reachability, CTF storage, and CAS reuse without changing the component descriptor.

## Wire Contract

The storage root is an OCI image manifest:

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "artifactType": "application/vnd.ocm.software.chunked-blob.v1",
  "config": {
    "mediaType": "application/vnd.ocm.software.chunked-blob.config.v1+json",
    "digest": "sha256:<config-json-digest>",
    "size": 186
  },
  "layers": [
    {
      "mediaType": "application/vnd.ocm.software.chunk.v1",
      "digest": "sha256:<chunk-digest>",
      "size": 4294967296
    }
  ]
}
```

The referenced config blob describes the logical content:

```json
{
  "content": {
    "mediaType": "application/vnd.example.vm.qcow2",
    "digest": "sha256:<complete-content-digest>",
    "size": 42949672960
  }
}
```

Contract:

* manifest layer order is content order; no chunk-index annotation;
* OCI layer descriptors carry each chunk's digest and size;
* `LocalBlob.localReference` is the chunk-manifest digest;
* `LocalBlob.mediaType` remains the logical content media type;
* resource digest remains the complete logical content digest;
* the complete digest is stable across chunk-size settings and storage representations;
* the config makes the graph self-describing, including for sources without a resource digest field;
* duplicate metadata must agree. Reader mismatch is an error;
* readers verify each chunk and the reassembled size and digest;
* generated `globalAccess` is suppressed for chunked blobs because an OCI reference does not provide the logical byte stream to standard clients.

Component normalisation already excludes resource and source access. A changed chunk manifest therefore does not change the signed component identity; the stable resource digest binds the logical bytes.

## Configuration

Introduce an OCI-specific central config because the representation is an OCI graph. It also applies to CTF, whose store implements that graph.

```yaml
type: oci.config.ocm.software/v1alpha1
maxChunkSize: 4Gi
```

`maxChunkSize` accepts a human-readable SI/IEC byte size or an integer byte count.

* omitted or zero: do not split new blobs;
* negative or malformed: configuration error;
* initial version has one global value and no host overrides or auto-detection;
* programmatic repository/provider options carry the resolved value;
* configuration applies to local resources and local sources, independent of their input method.

Chunking applies only to opaque local blobs. Native OCI artifacts and OCI image layouts keep their original manifests and layers. An oversized layer in a native artifact produces a clear target error; artifact rewriting is separate work.

## Write and Read Behavior

### New opaque blob

Use fixed-size chunks. Boundaries start at offset zero for each blob being split.

#### Chosen upload strategy: one pass with one temporary chunk

1. Open one source reader.
2. Stage at most one chunk in a temporary file while computing chunk and whole-content digests.
3. Rewind and push the chunk after its descriptor is known.
4. Repeat until EOF.
5. Push config and manifest.

For unknown-size input, stage up to `maxChunkSize + 1` bytes initially. Content at or below the threshold keeps the existing plain-blob representation. Larger content enters the split path. This bypasses the current full in-memory `ArtifactBlob.Buffer()` behavior.

Pros:

* reads the source once;
* works uniformly with OCI and CTF `content.Storage`;
* bounds RAM to the copy buffer and temporary disk to approximately one chunk.

Cons:

* requires temporary disk up to one chunk;
* uploads are sequential in the initial implementation;
* failed writes may leave unreferenced chunks for normal registry/CTF cleanup.

If a registry rejects a chunk below the configured policy, return a clear error with the target, attempted size, and configured limit. The initial implementation does not parse registry-specific errors or retry with progressively smaller chunks.

#### Alternative: two-pass reread and upload

First read the complete source to compute the whole-content and chunk descriptors. Reopen it and stream each known chunk directly to `content.Storage.Push`.

Pros:

* no temporary chunk files;
* uses the existing ORAS storage abstraction for OCI and CTF;
* constant RAM.

Cons:

* reads all source bytes twice;
* may download a remote source twice;
* repeats generated transformations such as compression and depends on byte-for-byte reproducibility;
* failures during the second pass waste the complete first pass.

Evaluate this strategy against representative file, compressed, generated, and remote blobs before implementation is finalized.

#### Alternative: registry-native upload while hashing

OCI Distribution permits initiating a blob upload, sending bytes with `PATCH` while calculating the digest, then finalizing with `PUT ?digest=...`.

Pros:

* one source read;
* no temporary chunk file;
* naturally supports upload progress and potentially resumability.

Cons:

* the current ORAS `content.Storage.Push` contract requires digest and size before upload;
* requires an ORAS extension/upstream API or registry-specific implementation;
* has no equivalent unknown-descriptor contract for CTF;
* introduces registry-specific retry, redirect, resumability, and cancellation behavior.

Do not bypass `content.Storage` in the initial implementation without this analysis.

### Existing chunked blob

`maxChunkSize` is a maximum accepted size, not a desired canonical layout.

* If every source chunk fits, preserve the manifest and chunks unchanged and use graph copy.
* If any chunk is oversized, preserve compatible chunks and split only oversized chunks into fixed-size pieces.
* Never coalesce compatible chunks merely because the target permits larger ones.
* If target splitting is disabled, preserve an already chunked graph.

This makes the resulting layout dependent on prior valid boundaries, intentionally. The rejected alternative is canonical rechunking of the complete logical stream from offset zero whenever one chunk is incompatible. Canonical rechunking would process all content, prevent direct concurrent copy of compatible chunks, change more digests, and lose target-side CAS reuse.

#### Alternative: Content-defined chunking

Content-defined chunking selects boundaries from the bytes instead of absolute offsets. A rolling fingerprint scans the stream; after a minimum size, a matching fingerprint cuts the chunk, while a hard maximum always forces a cut. FastCDC is a candidate algorithm.

This lets boundaries resynchronize after inserted or removed data. If most of a 50 GiB image remains byte-identical, a target may reuse the unchanged chunk digests and upload only the changed region—for example, about 10 GiB instead of the complete image. The saving depends on the image format and actual byte stability.

Pros:

* substantially better CAS reuse between related image versions;
* insertions do not shift every subsequent boundary;
* retains the registry-enforced maximum chunk size.

Cons:

* rolling-fingerprint CPU cost and additional implementation complexity;
* variable chunk sizes and versioned minimum/target/maximum parameters;
* limited reuse for compressed, encrypted, or broadly rewritten images;
* producers need matching algorithm profiles to obtain matching boundaries.

The existing manifest format already supports variable layer sizes; readers concatenate its ordered layers and do not need to understand the boundary algorithm. Content-defined chunking can therefore be added later without a new storage representation by extending the producer config with an optional versioned algorithm and parameters, for example:

```yaml
algorithm: fastcdc/v1
minChunkSize: 2Gi
averageChunkSize: 4Gi
maxChunkSize: 8Gi
```

The chunk config may record this generation profile for diagnostics and reproducibility, but reconstruction continues to depend only on the ordered layer descriptors and whole-content metadata. Fixed-size chunking remains the initial algorithm.

#### Follow-up: Built-in registry limits

Built-in per-registry limits are also deferred for research. OCM v1 carried a hard-coded GHCR limit, which improved zero-config UX but introduced stale-limit and maintenance risks. Evaluate a maintained limit table, explicit-config precedence, safety margins, update ownership, and advisory versus automatic behavior. Prefer advisory behavior until those questions are settled.

### Read

Resolve `localReference`, detect the chunk artifact, fetch layers in manifest order, and expose one concatenated `ReadOnlyBlob`. No OCI-layout tar or complete temporary file is needed. Integrity errors surface while consuming the stream.

## Storage and Transfer Integration

### Repository-Level Chunk Support

Repository implementations expose chunk graphs so compatible stores can use `oras.CopyGraph` without reconstruction. This layer owns:

* chunk format, split writes, transparent reads, and preserve/rechunk behavior;
* repository/provider hooks exposing chunked local blobs as graphs;
* equivalent graph storage in OCI registries and directory-backed CTFs.

### Graph-Native Transfer

Transfer orchestration detects chunk graph support and selects direct graph copy for local blobs. It provides:

* end-to-end OCI↔CTF and CTF↔CTF transfer without complete resource temp files;
* byte-stream fallback for unsplit or incompatible sources;
* transparent preservation of logical content metadata.

### CTF Archive Scope

CTF archive creation/extraction behavior is unchanged. Streaming applies to the OCI graph inside directory-backed CTFs.

## Compatibility

* Chunking is opt-in; existing unsplit writes and reads are unchanged.
* New clients read plain and chunked local blobs.
* Old clients are not required to read newly chunked blobs; they may expose an OCI-layout tar instead of logical bytes.
* Do not implement the OCM v1 comma-separated-reference representation.
* Document the minimum OCM version required for chunked content.

## Validation and Test Requirements

Cover at least:

* zero, below-limit, exact-limit, and limit-plus-one sizes;
* known- and unknown-size blobs, including compressed input;
* resources and sources;
* OCI and directory CTF stores;
* transparent byte-identical download;
* stable whole digest under different chunk settings;
* preservation of compatible chunk graphs;
* splitting only oversized existing chunks;
* metadata, per-chunk digest, whole digest, and whole size mismatches;
* cancellation and temporary-file cleanup;
* native OCI artifact/layout paths remaining unchanged;
* no full in-memory buffer or OCI-layout tar on the chunked blob path;
* backward-compatible reads of existing unsplit content.

## Consequences

* Large opaque local blobs can fit registry limits without changing logical resource identity.
* Chunk graph copies can deduplicate and avoid resource-level materialization after transfer orchestration selects the graph-native path.
* Initial upload uses temporary storage up to one chunk.
* Chunked content requires a chunk-aware OCM client.
* OCI-native artifact layers remain outside the feature's scope.
