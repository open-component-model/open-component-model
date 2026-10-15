---
title: "OCI Resource Digests"
description: "How OCM records and verifies OCI manifest digests while preserving historical descriptors."
weight: 10
toc: true
---

An OCM resource digest records a hash algorithm, a normalisation algorithm, and
a value. All three fields affect the signed component descriptor and component
reference digests. Changing the normalisation name changes the descriptor's
identity, even if the hash value stays the same. See
[Signing and Verification]({{< relref "docs/concepts/signing-and-verification-concept.md" >}})
for how resource digests participate in signatures.

For OCI artifacts, the normalisation identifies what was hashed:

| Normalisation | Hashed content |
| --- | --- |
| `ociArtifactDigest/v1` | Selected OCI manifest or multi-platform index, regardless of how an OCI layout archive is serialized. |
| `genericBlobDigest/v1` | Bytes returned by a non-OCI blob access. For an archive, this is the archive checksum, not its OCI manifest digest. |

See the specification's [artifact normalisation types](https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/04-algorithms/artifact-normalization-types.md).

A newly created OCI resource records the manifest digest like this:

```yaml
digest:
  hashAlgorithm: SHA-256
  normalisationAlgorithm: ociArtifactDigest/v1
  value: 262578cde928d5c9eba3bce079976444f624c13ed0afb741d90d5423877496cb
```

## Compatibility with Existing Descriptors

Earlier OCM v2 OCI writers recorded a manifest or index hash under
`genericBlobDigest/v1`. The descriptor schema version does not distinguish these
writers. OCM verifies complete historical digests against the OCI root and
preserves their original fields, rather than silently relabeling them and
invalidating signatures or component references.

| Existing resource digest | OCI processing and upload |
| --- | --- |
| Absent or empty | Generate `ociArtifactDigest/v1` from the OCI root. |
| Incomplete | Verify the populated fields, then complete the digest with `ociArtifactDigest/v1`; reject mismatches. |
| Complete `ociArtifactDigest/v1` | Verify the hash algorithm and value against the OCI root; preserve the digest. |
| Complete historical `genericBlobDigest/v1` | Accept only when the hash algorithm and value match the OCI root; preserve the digest. |
| Complete with another normalisation | Reject during OCI processing or upload. |

OCI layout packing preserves complete digests with other normalisations without
verifying them. An incomplete historical generic label is completed using
`ociArtifactDigest/v1`. The historical exception applies only to OCI artifacts:
an archive-byte checksum is not accepted as a manifest digest, and ordinary
blob verification is unchanged.

Helm chart digests follow the same distinction: HTTP chart repositories hash
chart archive bytes with `genericBlobDigest/v1`, while `oci://` chart references
and charts converted to OCI layouts identify a manifest with
`ociArtifactDigest/v1`. Complete historical generic labels on OCI chart
references are verified against the manifest and preserved.

## Archive Checksums and Digest Pins

An OCI layout archive has its own byte checksum, separate from the resource's
manifest digest. OCM does not compare the archive-byte checksum with the
manifest digest or rewrite the resource digest when it records the archive
checksum. Ordinary blobs still require a valid, matching byte checksum.

A digest pin in the target OCI access must match the incoming manifest or index.
OCI resource upload rejects a mismatch before updating the destination tag.

OCM can record OCI root digests using SHA-256 or SHA-512. Other root algorithms
can be transported as source artifacts, but not recorded as resource digests.

## Impact on Existing Components

- Existing valid OCI descriptors retain their `ociArtifactDigest/v1` triple
  during resource-copying transfers.
- Existing historical OCI descriptors retain their generic label through OCI
  processing, upload, and layout packing. They are not migrated automatically.
- Newly created OCI descriptors use `ociArtifactDigest/v1`. Their component
  digests therefore differ from descriptors made from the same constructor
  input by earlier writers.
- Wget, HTTP, and other generic blob consumers do not gain a legacy exception.
  An OCI-layout archive's manifest digest is not its HTTP payload checksum.
- Rebuilding a published descriptor with corrected metadata requires new
  signatures and updated dependent component reference digests. Consumers
  must not rewrite published descriptors in place.
