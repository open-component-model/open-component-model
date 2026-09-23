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

The OCI repository therefore uses the following policy:

| Operation                                                             | Behavior                                                                                                            |
| --------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| Generate a missing digest for an OCI artifact                         | Write `ociArtifactDigest/v1` with the manifest/index hash.                                                          |
| Generate a missing digest for an ordinary blob                        | Write `genericBlobDigest/v1` with the blob byte checksum.                                                           |
| Process or upload an OCI artifact with an existing OCI digest         | Compare its hash algorithm and value with the resolved/copied OCI root; preserve the entire triple.                 |
| Process or upload an OCI artifact with an existing generic digest     | Accept the historical label only when its hash algorithm and value match the OCI root; preserve the entire triple.  |
| Process or upload an OCI artifact with another nonempty normalization | Return an unsupported-normalization error.                                                                          |
| Upload with incomplete digest metadata (empty normalization)          | Regenerate a complete `ociArtifactDigest/v1` triple from the copied root, retaining the base upload fix's behavior. |
| Process an existing digest with empty normalization                   | Return an unsupported-normalization error.                                                                          |
| Pack an existing normalized resource into local storage               | Preserve the existing digest; do not replace it with a storage wrapper's digest.                                    |

The legacy exception is explicit in `oci/internal/digest.VerifyOCIArtifact` and
is used only by the OCI resource processing/upload paths. It is not an alias in
generic blob verification. It does not allow a mismatching value or hash
algorithm, and it does not identify which software originally wrote a descriptor.
For an explicit OCI artifact access, a generic archive-byte checksum is not
accepted as a manifest hash. The upload fallback for empty normalization applies
to incomplete transport metadata, not to complete published legacy digests;
regenerating such incomplete metadata is not guaranteed to preserve a signature
that already covered it.

`ArtifactBlob` exposes byte checksums independently of resource digest metadata.
An OCI-layout archive's known byte checksum is never compared directly with its
resource manifest digest. Setting or caching a byte checksum does not rewrite a
resource digest. Ordinary generic blobs still require matching byte checksums.

## Impact and limits

- **Existing valid OCI descriptors:** their `ociArtifactDigest/v1` triple and
  component/resource creation times survive resource-copying transfers.
- **Existing legacy v2 descriptors:** OCI processing and upload retain the
  historical generic label, so unchanged signatures and parent references remain
  valid. No automatic migration is performed.
- **New descriptors:** OCI resources receive the correct normalization. Their
  component digests can differ from descriptors produced by the old implementation
  from otherwise identical constructor input. Consumers must use the digest of
  the newly built descriptor rather than assuming an old digest still applies.
- **Local blobs and CTF:** storage integrity and normalized resource verification
  are distinct. Packing preserves existing normalized metadata, including when a
  storage wrapper index differs from the resource manifest. Successful packing
  is not a claim that every supported external normalization has been verified.
- **Descriptor verification:** validating a descriptor's signature does not by
  itself verify all resource bytes. Artifact graph copying and byte verification
  remain necessary.
- **Wget, HTTP, and other generic blob consumers:** no legacy exception is added.
  Downloading an OCI-layout archive does not turn its manifest digest into an HTTP
  payload checksum. Supporting OCI artifact normalization through these access
  methods remains separate work; removing verification or substituting a tar hash
  would not be a signature-preserving fix.
- **Migration:** a publisher may rebuild with correct metadata, but must regenerate
  signatures and update dependent reference digests. Consumers must not rewrite
  published descriptors in place.

## Regression coverage

The signed CLI integration test
`Test_Integration_Transfer_OCIArtifact_PreservesV1DescriptorDigest` exercises both
normalization labels, registry-to-registry and registry-to-CTF-to-registry routes,
and both `ociArtifact` and `localBlob` upload modes. It signs child and parent
components with RSA after their metadata is finalized, verifies signatures at
each hop, compares parent reference digests, and checks the downloaded OCI root
and layer bytes.

OCI repository unit tests cover new and existing digests for single manifests
and multi-platform indexes, unsupported normalization, wrong hash algorithms,
wrong values, and input immutability. Blob and packing tests cover known archive
checksums, metadata preservation during buffering, rejection of corrupt layers,
and ordinary generic-blob checksum mismatches.

Run the focused tests from `bindings/go/`:

```sh
go test ./oci/... -short -skip Integration -count=1
go test -race ./oci/blob ./oci/internal/digest ./oci/internal/pack
go test ./cli/integration -run '^Test_Integration_Transfer_OCIArtifact_PreservesV1DescriptorDigest$' -count=1 -timeout=5m
```

The CLI integration test requires Docker for its two local registries, but no
public OCI image or external signing service.
