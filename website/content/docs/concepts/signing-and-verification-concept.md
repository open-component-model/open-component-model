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
| --- | --- |
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
        type: LocalBlob/v1
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
        type: OCIImage/v1
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

OCM's `signature.algorithm` field selects between two signing approaches: classical RSA signatures over a long-lived key pair, and [Sigstore](https://www.sigstore.dev/)-based keyless signing, where each signature is made with a fresh, short-lived key bound to your OIDC identity. The two approaches differ not just in cryptography but in their trust model — see [Trust Models](#trust-models) below.

| Algorithm | Type | Trust Model | Characteristics |
| --------- | ---- | ----------- | --------------- |
| RSASSA-PSS (default) | Asymmetric (RSA) | Public key or certificate chain | Probabilistic, stronger security guarantees, recommended for new RSA-based implementations |
| RSA-PKCS#1 v1.5 | Asymmetric (RSA) | Public key or certificate chain | Deterministic, widely supported, compatible with legacy systems |
| Sigstore (keyless, early access) | Asymmetric (ECDSA, ephemeral) | OIDC identity | Short-lived certificate from Fulcio bound to your OIDC identity, transparency-log entry in Rekor; no long-lived keys to manage |

To override the default signing algorithm or encoding policy, see the `--signer-spec` flag in the [CLI reference]({{< relref "/docs/reference/ocm-cli/ocm_sign_component-version.md" >}}).
The signer spec file configures only the algorithm and encoding policy — credentials are always resolved separately via the [`.ocmconfig`]({{< relref "configure-multiple-credentials.md" >}}) file.

For RSA-based signing, OCM uses PEM-encoded key files configured in the `.ocmconfig`:

- **Private keys**: Used by producers to sign component versions
- **Public keys**: Distributed to consumers for verification

See [How-to: Generate Signing Keys]({{< relref "generate-signing-keys.md" >}}) for creating RSA key pairs.

Sigstore-based signing has no long-lived keys: a fresh signing key is generated per signature and certified by Fulcio against your OIDC identity. See [Sigstore (Keyless)](#sigstore-keyless) below.

### Signature Encoding Policies

The `signatureEncodingPolicy` in the [signer spec]({{< relref "/docs/reference/ocm-cli/ocm_sign_component-version.md" >}}) controls how the **signature output** is serialized and stored. It does **not** affect the format of key input files, which are always PEM-encoded.

| Policy | Signature Format | Media Type | Certificate Chain | Verification Requires |
| ------ | ---------------- | ---------- | ----------------- | --------------------- |
| **Plain** (default) | Hex-encoded raw bytes | `application/vnd.ocm.signature.rsa.pss` | Not embedded | Externally supplied public key |
| **PEM** (early access) | PEM `SIGNATURE` block + `CERTIFICATE` blocks | `application/x-pem-file` | Embedded in signature | Valid certificate chain in signature |

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

#### PEM Encoding (Early Access)

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

{{< callout context="note" title="PEM encoding is in early access" icon="outline/info-circle" >}}
PEM encoding is currently being rolled out across projects and we are awaiting feedback. The interface may evolve based on that feedback.
{{< /callout >}}

{{< callout context="note" title="Key files vs. signature encoding" icon="outline/info-circle" >}}
A common source of confusion: "PEM" in `signatureEncodingPolicy` refers to the **signature output** format, not the key input format. Input keys are **always** PEM-encoded files (e.g. `-----BEGIN RSA PRIVATE KEY-----`), regardless of which encoding policy is selected.

When using PEM encoding for signing, the credential referenced by `public_key_pem` / `public_key_pem_file` must contain **X.509 certificates** (not bare public keys), because the certificate chain is embedded into the signature for self-contained verification.
{{< /callout >}}

### Sigstore (Keyless)

Sigstore replaces the long-lived key pair at the heart of RSA signing with a short-lived certificate bound to your OIDC identity.
The signer logs in to an identity provider; Sigstore issues a certificate valid for ~10 minutes; the signature is recorded in a public transparency log.
Verifiers don't pin a public key — they declare which identity they trust.

The Sigstore stack is four cooperating pieces. For the public-good happy path (`sigstore.dev`) you don't deploy any of them;
for an enterprise stack a platform team has already deployed them and you point your config at their endpoints.

- **OIDC IdP** — the identity provider you log in to (Google, GitHub, Microsoft, or your corporate IdP). The token it issues proves *who* is signing.
- **Fulcio** — a short-lived certificate authority. It accepts your OIDC token and issues a certificate (valid for ~10 minutes) that binds the token's identity claims to a fresh signing key.
- **Rekor** — an append-only public transparency log. Every signature is recorded so anyone can audit when, and by whom, it was made.
- **TUF** — the mechanism clients use to discover the current trusted roots for Fulcio and Rekor. The verifier uses it to know which CA and which log to trust.

The signature payload stored in the component descriptor is a Sigstore bundle: the signature bytes, the Fulcio certificate,
and the Rekor inclusion proof, all in one self-contained blob. This self-containment is a key design principle of Sigstore:
the signature value includes everything a verifier needs to validate the signature and establish trust in the signer's identity,
without needing any out-of-band information.

This matters directly for OCM's sovereign-delivery and air-gapped scenarios. The component version is the unit of transport,
and OCM carries the proof of authorship along with it: the signed descriptor and the bundle embedded inside it travel as one.

The only piece a verifier needs in addition is a local trusted-root file — the public keys of the Fulcio CA and the Rekor
instance the component was signed against, whether that is public-good Sigstore or an enterprise stack — distributed into the
disconnected environment once, out of band.

With those pieces in place, `ocm verify cv` runs entirely offline: no callback to
any Sigstore service, no TUF refresh, no network egress whatsoever. This is what makes Sigstore viable in regulated and
sovereign-cloud deployments where egress to public or on-premise infrastructure is not permitted at verification time.

```yaml
signature:
  algorithm: sigstore
  mediaType: application/vnd.dev.sigstore.bundle.v0.3+json
  value: <base64-encoded Sigstore bundle>
```

Because the certificate is in the bundle and the trust roots come from TUF (or an enterprise trusted-root file), no encoding-policy choice is involved — Sigstore is its own algorithm with its own bundle format.

For how this changes what verifiers pin, see [Identity-Based Trust](#identity-based-trust-sigstore) below.

{{< callout context="note" title="Sigstore signing is in early access" icon="outline/info-circle" >}}
Sigstore (keyless) signing is currently being rolled out and we are awaiting feedback. The interface may evolve based on that feedback.
{{< /callout >}}

## Trust Models

The signing approach you choose determines how verifiers establish trust in a signature. RSA-based algorithms support two models depending on encoding policy (key pinning or certificate chain); Sigstore brings a third, identity-based model. OCM supports three trust models in total.

### Key Pinning (Plain Encoding)

The verifier explicitly configures the signer's public key in `.ocmconfig`. Trust is established by knowing the exact key that was used to sign.

- No PKI infrastructure required
- Simple to set up for small teams or self-signed workflows
- Verifier must obtain the public key out-of-band (e.g., from a secrets manager or shared repository)
- Rotating keys requires updating every verifier's configuration

### Certificate Chain Trust (PEM Encoding)

The signer embeds the certificate chain (leaf + any intermediates) directly in the signature value. The verifier pins only the root CA certificate as a trust anchor.

- Requires PKI infrastructure (or a locally generated CA)
- Verifier only needs the root CA -- leaf certificates can change without reconfiguring verifiers
- Supports organizational delegation: the root CA can issue intermediate CAs for different teams
- The root CA is never embedded in the signature; OCM rejects self-signed certificates found in the embedded chain

### Identity-Based Trust (Sigstore)

The signer authenticates to an OIDC identity provider; Fulcio binds that identity to a short-lived certificate; Rekor records the signature in a public transparency log. The verifier doesn't pin a key or a CA — it declares **which OIDC identity** it trusts.

- No long-lived keys to manage, distribute, or rotate
- Trust anchor is the signer's identity (e.g. `jane.doe@example.com` via `https://github.com/login/oauth`), not a public key
- Public-good Sigstore (`sigstore.dev`) is shared infrastructure; an enterprise stack runs Fulcio/Rekor/TUF inside the organization
- Audit trail is automatic and public (or organization-internal for enterprise stacks)

### When to Use Each

| Criterion | Plain (Key Pinning) | PEM (Certificate Chain) | Sigstore (Identity) |
| --------- | ------------------- | ----------------------- | ------------------- |
| Long-lived keys to manage | Yes | Yes (issued by your CA) | No |
| PKI infrastructure available | No | Yes | N/A (Fulcio replaces it) |
| Number of signers | Few | Many or changing | Anyone with an OIDC identity |
| Key rotation complexity | High (update all verifiers) | Low (root CA stays stable) | None (certs are short-lived) |
| Signature is self-contained | No (public key needed separately) | Yes | Yes (bundle includes cert + log proof) |
| Audit trail | Build your own | Build your own | Built-in (Rekor) |
| Recommended for | Simple setups, personal projects | Enterprise environments with existing PKI | Teams that want to skip key management entirely |

For hands-on steps, see [Tutorial: Plain Signatures]({{< relref "docs/tutorials/signing/plain.md" >}}) and [Tutorial: Certificate Chains (PEM)]({{< relref "docs/tutorials/signing/pem.md" >}}).

## Next Steps

- [How-to: Generate Signing Keys]({{< relref "generate-signing-keys.md" >}}) - Step-by-step creating RSA key pairs.
- [How-to: Configure Signing Credentials]({{< relref "configure-signing-credentials.md" >}}) - Set up OCM to use your keys for signing and verification
- [How-to: Sign a Component Version]({{< relref "sign-component-version.md" >}}) - Step-by-step signing instructions
- [How-to: Verify a Component Version]({{< relref "verify-component-version.md" >}}) - Step-by-step verification instructions

## Related Documentation

- [Concept: Component Identity]({{< relref "component-identity.md" >}}) - Understanding component structure
