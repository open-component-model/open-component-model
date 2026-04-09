---
title: "Signing and Verification"
description: "Understanding how OCM ensures component integrity and authenticity through cryptographic signatures."
weight: 3
toc: true
---

OCM uses cryptographic signatures to guarantee that component versions are authentic (created by a trusted party) and have not been tampered with during storage or transfer.

## Why Sign Components?

The software lifecycle involves  multiple stages: development, build, packaging, distribution, and deployment. At each stage, components could potentially be:

- **Modified** — malicious actors could inject code or alter resources
- **Replaced** — components could be swapped for compromised versions
- **Misattributed** — components could falsely claim to come from a trusted source

Signing addresses these risks by creating a cryptographic proof of:

1. **Integrity**: The component has not changed since it was signed
2. **Authenticity**: The signature was created by someone with access to the private key
3. **Provenance**: The signer cannot deny having signed the component

## How OCM Signing Works

```mermaid
flowchart TB
    subgraph sign ["Sign (Producer)"]
        direction TB
        A[Component Version] --> B[Normalize & Hash]
        B --> C[Sign with Private Key]
        C --> D["Signature embedded in CV"]
    end
    
    sign --> T["Transfer Component Version"]
    
    T --> verify
    
    subgraph verify ["Verify (Consumer)"]
        direction TB
        E[Component Version] --> F[Extract Signature]
        E --> G[Normalize & Hash]
        F --> H[Verify with Public Key]
        G --> H
        H --> I{Valid?}
        I -->|Yes| VALID["✓ Trusted"]
        I -->|No| INVALID["✗ Rejected"]
    end
```

### Normalization and Digest Calculation

OCM uses a two-layer approach to ensure consistent and reproducible digests:

#### Component Descriptor Normalization

Before hashing, the component descriptor is normalized into a canonical form, eliminating any ambiguities
that could cause the same logical descriptor to produce different digests. The default normalization
algorithm ([`jsonNormalisation/v4alpha1`](https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/04-algorithms/component-descriptor-normalization-algorithms.md#normalization-algorithms)) defines exactly how this canonical form is derived, ensuring
identical component descriptors always yield the same digest.

This entire approach relies on the fact that **content digests** are preserved across transfers.
Each resource's digest is computed from its actual content (not from where it is stored),
and this digest is recorded in the component descriptor. When a component version is transported to a different registry,
the `access` references change, but the content — and therefore its digest — remains the same.
This is why the signature remains valid after transfer.

#### Artifact Digest Normalization

OCM supports different digest algorithms for different artifact types.
The algorithm determines how a resource's content is hashed to produce its digest:

| Algorithm | Description |
|-----------|-------------|
| `genericBlobDigest/v1` | Direct hash of blob content. For OCI artifacts (container images, Helm charts), this is the hash of the top-level OCI manifest, ensuring consistency with OCI registry behavior. For non-OCI content (executables, blueprints), it is the direct hash of the raw blob. |
| `ociArtifactDigest/v1` | Computes the digest of the OCI manifest specifically. Effectively equivalent to `genericBlobDigest/v1` for OCI content. You may encounter this algorithm in older component descriptors. |

While the architecture allows for multiple digest algorithms, in practice **`genericBlobDigest/v1` is the only algorithm currently used** across all artifact types.

#### Recursive Component References

When a component version is **signed**, the digests of referenced components are calculated and embedded into the component descriptor.
This does **not** happen automatically when references are created — without signing, references can exist without digests,
and in that case there is no integrity guarantee for the referenced components.

```yaml
references:
  - componentName: ocm.software/helper
    name: helper
    version: 1.0.0
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: jsonNormalisation/v4alpha1
      value: 01c211f5c9cfd7c40e5b84d66a2fb7d19cb0...
```

Once signed, this creates a **complete integrity chain** — verifying the root component automatically verifies all transitive dependencies.

### What Gets Signed?

OCM signs a **digest** of the normalized component descriptor (see [Normalization and Digest Calculation](#normalization-and-digest-calculation)
for how the canonical form is derived). The signed digest covers:

- Component metadata (name, version, provider)
- Resource descriptors (including digest, if available)
- Source descriptors (including digest, if available)
- Component references (including digest, if available)
- Labels marked with `signing: true` (at any level)

Labels without `signing: true` are excluded from the digest and do not affect the signature.
Storage-related fields like `access` and `repositoryContexts` are also excluded — see [Normalization and Digest Calculation](#normalization-and-digest-calculation) above.

The signature does **not** cover the raw resource content directly — instead, it covers the **digests** of those resources as recorded in the component descriptor. The `access` field (which describes *where* a resource is stored) is **excluded** from the signed digest. This is a key design principle:

- **Location-independent integrity** — a component version can be transferred to a different registry (changing all `access` references) without invalidating its signature. The digest remains stable because it depends only on *what* the artifacts contain, not *where* they are stored.
- Any change to resource content changes its digest, invalidating the signature.
- Signature verification is fast (no need to re-hash large binaries).

This separation of content identity from storage location is what enables secure delivery across environments: a producer signs a component version once, and consumers can verify it after any number of transfers — even into air-gapped environments with completely different registries.

The following example shows a signed component descriptor. Notice that each resource has both an `access` field (storage location) and a `digest` field (content hash). Only the `digest` is included in the signature — the `access` can change freely during transfers:

{{< details "Example Signed Component Descriptor" >}}
```yaml
component:
  name: github.com/acme.org/helloworld
  version: 1.0.0
  provider: acme.org
  resources:
    - name: mylocalfile
      type: blob
      version: 1.0.0
      relation: local
      access:                          # NOT included in signature
        type: localBlob
        localReference: sha256:70a257...
        mediaType: text/plain; charset=utf-8
      digest:                          # Included in signature
        hashAlgorithm: SHA-256
        normalisationAlgorithm: genericBlobDigest/v1
        value: 70a2577d7b649574cbbba99a2f2ebdf27904a4abf80c9729923ee67ea8d2d9d8
    - name: image
      type: ociImage
      version: 1.0.0
      relation: external
      access:                          # NOT included in signature
        type: ociArtifact
        imageReference: ghcr.io/stefanprodan/podinfo:6.9.1@sha256:262578cd...
      digest:                          # Included in signature
        hashAlgorithm: SHA-256
        normalisationAlgorithm: genericBlobDigest/v1
        value: 262578cde928d5c9eba3bce079976444f624c13ed0afb741d90d5423877496cb
signatures:
  - name: default
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: jsonNormalisation/v4alpha1
      value: 91dd197868907487e62872695db1fa7b397fde300bcbae23e24abc188fb147ad
    signature:
      algorithm: RSASSA-PSS
      mediaType: application/vnd.ocm.signature.rsa.pss
      value: 7feb449229c6ffe368144995432befd1505d2d29...
```
{{< /details >}}

### Signature Storage

Signatures are stored as part of the component version:

```yaml
signatures:
  - name: acme-release-signing
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: jsonNormalisation/v4alpha1
      value: abc123...
    signature:
      algorithm: RSASSA-PSS
      mediaType: application/vnd.ocm.signature.rsa
      value: <base64-encoded-signature>
```

A component version can have **multiple signatures** from different parties, enabling:

- Separation of build and release signing
- Multiple approval workflows
- Cross-organizational trust chains

## Supported Signing Algorithms

OCM currently only supports RSA-based signing algorithms:

| Algorithm            | Type | Characteristics |
|----------------------|------|-----------------|
| RSASSA-PSS (default) | Asymmetric | Probabilistic, stronger security guarantees, recommended for new implementations |
| RSA-PKCS#1 v1.5      | Asymmetric | Deterministic, widely supported, compatible with legacy systems |

To override the default signing algorithm or encoding policy, see the `--signer-spec` flag in the [CLI reference]({{< relref "/docs/reference/ocm-cli/ocm_sign_component-version.md" >}}).
The signer spec file configures only the algorithm and encoding policy — credentials are always resolved separately via the [`.ocmconfig`]({{< relref "configure-multiple-credentials.md" >}}) file.

For key management, OCM uses PEM-encoded key files configured in the `.ocmconfig`:

- **Private keys**: Used by producers to sign component versions
- **Public keys**: Distributed to consumers for verification

See [How-to: Generate Signing Keys]({{< relref "docs/how-to/generate-signing-keys.md" >}}) for creating RSA key pairs.

{{< callout context="tip" title="Upcoming Sigstore Support" icon="outline/bulb" >}}
We are planning to add support for [Sigstore](https://www.sigstore.dev/) and Cosign as an additional signing mechanism.
This will enable keyless signing workflows and improved supply chain security. Stay tuned for updates.
{{< /callout >}}

### Signature Encoding Policies

The `signatureEncodingPolicy` in the [signer spec]({{< relref "/docs/reference/ocm-cli/ocm_sign_component-version.md" >}}) controls how the **signature output** is serialized and stored. It does **not** affect the format of key input files, which are always PEM-encoded.

| Policy | Signature Format | Media Type | Certificate Chain | Verification Requires |
|--------|-----------------|------------|-------------------|-----------------------|
| **Plain** (default) | Hex-encoded raw bytes | `application/vnd.ocm.signature.rsa.pss` | Not embedded | Externally supplied public key |
| **PEM** (experimental) | PEM `SIGNATURE` block + `CERTIFICATE` blocks | `application/x-pem-file` | Embedded in signature | Valid certificate chain in signature |

#### Plain Encoding (Default)

The raw RSA signature bytes are hex-encoded and stored directly. This is the most compact representation.
Verification always requires the public key to be provided separately via `.ocmconfig` credentials.

Example signature in a component descriptor:

```yaml
signature:
  algorithm: RSASSA-PSS
  mediaType: application/vnd.ocm.signature.rsa.pss
  value: d1ea6e0cd850c8dbd0d20cd39b9c7954...
```

#### PEM Encoding (Experimental)

The signature is wrapped in a PEM block of type `SIGNATURE`, optionally followed by the signer's X.509 certificate chain.
This makes the signature **self-contained**: verifiers can extract and validate the public key from the embedded chain
without needing a separately distributed key.

Example of a PEM-encoded signature value:

```yaml
signature:
      algorithm: RSASSA-PSS
      mediaType: application/x-pem-file
      value: |
        -----BEGIN SIGNATURE-----
        Signature Algorithm: RSASSA-PSS
        
        <base64-encoded signature bytes>
        -----END SIGNATURE-----
        -----BEGIN CERTIFICATE-----
        <leaf certificate>
        -----END CERTIFICATE-----
        -----BEGIN CERTIFICATE-----
        <intermediate CA, if applicable>
        -----END CERTIFICATE-----
```

{{< callout context="caution" title="PEM encoding is experimental" icon="outline/alert-triangle" >}}
This encoding policy may change or be deprecated in future versions. For production use, prefer the default Plain encoding.
{{< /callout >}}

{{< callout context="note" title="Key files vs. signature encoding" icon="outline/info-circle" >}}
A common source of confusion: "PEM" in `signatureEncodingPolicy` refers to the **signature output** format, not the key input format. Input keys are **always** PEM-encoded files (e.g. `-----BEGIN RSA PRIVATE KEY-----`), regardless of which encoding policy is selected.

When using PEM encoding for signing, the credential referenced by `public_key_pem` / `public_key_pem_file` must contain **X.509 certificates** (not bare public keys), because the certificate chain is embedded into the signature for self-contained verification.
{{< /callout >}}

## Next Steps

- [How-to: Generate Signing Keys]({{< relref "generate-signing-keys.md" >}}) - Step-by-step creating RSA key pairs.
- [How-to: Configure Signing Credentials]({{< relref "configure-signing-credentials.md" >}}) - Set up OCM to use your keys for signing and verification
- [How-to: Sign a Component Version]({{< relref "sign-component-version.md" >}}) - Step-by-step signing instructions
- [How-to: Verify a Component Version]({{< relref "verify-component-version.md" >}}) - Step-by-step verification instructions

## Related Documentation

- [Concept: Component Identity]({{< relref "component-identity.md" >}}) - Understanding component structure
