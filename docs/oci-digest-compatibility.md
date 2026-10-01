# OCI resource digest compatibility

## Artifact identity and transport checksums

An OCM resource digest consists of its hash algorithm, normalization algorithm,
and value. The complete triple participates in descriptor signing and component
reference digests. Changing only the normalization name changes the signed
component, even when the hash value stays the same.

For OCI artifacts, these values describe different representations:

- `ociArtifactDigest/v1`: the selected artifact manifest or multi-platform index
  digest, independent of how an OCI layout archive is serialized.
- `genericBlobDigest/v1`: the checksum of the blob bytes returned by the access
  method. For an archive this is the archive checksum, not the manifest digest.

See the specification's [artifact normalization types](https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/04-algorithms/artifact-normalization-types.md).

## Compatibility policy

Earlier v2 OCI writers recorded a manifest/index hash with
`genericBlobDigest/v1`. These published descriptors cannot be silently relabeled:
that would invalidate signatures and component references. The descriptor schema
version alone cannot identify the writer or distinguish this historical behavior.

OCI resource processing, upload, and layout packing therefore apply one rule:

- **Absent or empty:** generate `ociArtifactDigest/v1` from the OCI root, or
  `genericBlobDigest/v1` from the bytes of an ordinary blob.
- **Incomplete (some fields empty):** fields that are set must match; the
  missing fields are completed. Mismatches are rejected.
- **Complete, `ociArtifactDigest/v1`:** hash algorithm and value must match the
  OCI root; the triple is preserved.
- **Complete, historical `genericBlobDigest/v1` on OCI:** accepted only when hash
  algorithm and value match the OCI root; the triple is preserved.
- **Complete, other normalization:** rejected by processing and upload. Layout
  packing preserves it unverified.

Incomplete metadata cannot carry a valid signature, so completing it never
invalidates one. A historical generic label on an incomplete OCI digest is
corrected to `ociArtifactDigest/v1`.

The legacy exception lives in `oci/internal/digest.VerifyOCIArtifact`. It is not
an alias in generic blob verification, and it never accepts an archive-byte
checksum as a manifest hash.

`ArtifactBlob` exposes byte checksums independently of resource digest metadata.
An OCI-layout archive's byte checksum is never compared with its resource manifest
digest, and setting a byte checksum does not rewrite the resource digest. Ordinary
blobs still require a valid, matching byte checksum: a malformed or incomplete
`genericBlobDigest/v1` is rejected, and buffering re-verifies the content.

Both resource upload paths validate or complete the digest before copying or
tagging. A digest pin in the target access must also match the incoming OCI root.
A rejection leaves the existing destination tag and content intact; this is not
a general transaction guarantee for failures during graph copying.

The OCI-to-OCM digest mapping supports SHA-256 and SHA-512. Other root algorithms
can be transported for sources but cannot be recorded as resource digests.

## Impact and limits

- **Existing valid OCI descriptors:** their `ociArtifactDigest/v1` triple and
  component/resource creation times survive resource-copying transfers.
- **Existing legacy v2 descriptors:** OCI processing, upload, and layout packing
  retain the historical generic label, so unchanged signatures and parent
  references remain valid. No automatic migration is performed.
- **New descriptors:** OCI resources receive the correct normalization. Their
  component digests differ from descriptors produced by the old implementation
  from otherwise identical constructor input.
- **Wget, HTTP, and other generic blob consumers:** no legacy exception is added.
  Downloading an OCI-layout archive does not turn its manifest digest into an HTTP
  payload checksum. Supporting OCI artifact normalization through these access
  methods remains separate work.
- **Migration:** a publisher may rebuild with correct metadata, but must regenerate
  signatures and update dependent reference digests. Consumers must not rewrite
  published descriptors in place.
