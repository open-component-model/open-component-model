# Next Generation Component Constructor Support

* **Status**: proposed
* **Deciders**: OCM Technical Steering Committee
* **Date**: 2025.08.13

**Technical Story**:
Provide a consistent, pluggable way to sign and verify component descriptors based on a normalized representation.

---

## Context and Problem Statement

To verify the integrity of a component version, users run:

```shell
ocm verify componentversion --signature mysig --verifier mypublickey ghcr.io/open-component-model/ocm//ocm.software/ocm:0.17.0
```

This does:

1. Download the component version descriptor from the repository.
2. Inspect the [`signatures`](https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/03-elements-sub.md#signatures) field:

   ```yaml
   signatures:
     - name: mysig
       digest:
         hashAlgorithm: sha256
         normalisationAlgorithm: jsonNormalisation/v1
         value: cf08abae08bb874597630bc0573d941b1becc92b4916cbe3bef9aa0e89aec3f6
       signature:
         algorithm: RSASSA-PKCS1-V1_5
         mediaType: application/vnd.ocm.signature.rsa
         value: 390157b7...75ab2705d6
   ```
3. Verify the signature using the configured verifier from `.ocmconfig`.

Signing uses the analogous command:

```shell
ocm sign componentversion --signature mysig --signer myprivatekey ghcr.io/open-component-model/ocm//ocm.software/ocm:0.17.0
```

This downloads, normalizes, signs, and re-uploads the descriptor. Signing and verification configs live in `.ocmconfig`.

---

## Decision Drivers

* **Simplicity**: Keep signing and verification decoupled from normalization internals.
* **Extensibility**: Support new algorithms and key types via plugins.
* **Maintainability**: Clear contracts and separation of concerns to enable testing.

---

## Outcome

Implement a plugin-driven signing/verification system based on a **ComponentSignatureHandler** contract to compute normalized digests and sign/verify them.

---

## Contract Structure

**Module**

```text
bindings/go/descriptor/signature
```

**Responsibilities**

* Working with `ComponentSignatureHandler` implementations defined by an interface.
* Config parsing and resolution.
* Orchestration of normalization, digest calculation, signing, and verification.

### Adding new Signing / Verification Handlers

**Module**

```text
bindings/go/<technology>/go.mod
```

**Package**

```text
bindings/go/<technology>/signing/method
```

Example for `RSA-PSS` signing:

```text
bindings/go/rsa/go.mod
bindings/go/rsa/signing/pss
```

---

## `ComponentSignatureHandler` Contract

> Contract name used in code: `ComponentSignatureHandler`.

```go
package handler

import (
    "context"

    "ocm.software/open-component-model/bindings/go/blob"
    descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
    "ocm.software/open-component-model/bindings/go/runtime"
)

// ComponentSignatureHandler groups signing and verification.
// Implementations MUST be able to verify descriptors they produce via Sign.
type ComponentSignatureHandler interface {
    ComponentSignatureSigner
    ComponentSignatureVerifier
}

// ComponentSignatureSigner signs a normalized Component Descriptor.
//
// Implementations MUST:
// - Expect that ALL contained digests were already precomputed from scratch for artifacts and component references BEFORE calling Sign.
// - Compute the component-version digest over the normalized descriptor.
// - Use a registered normalization algorithm and artifact digesting per configuration.
// - Produce a signature envelope that records: algorithm, media type, value, optional issuer, and name.
// See: https://ocm.software/docs/getting-started/sign-component-versions/
type ComponentSignatureSigner interface {
    // Sign signs the descriptor using the provided config.
    // An extensible config SHOULD support:
    //   - signature name(s) and normalization algorithm id,
    //   - key material (private keys), issuer information,
    //   - media type and algorithm selection.
    Sign(ctx context.Context, descriptor descruntime.Descriptor, config runtime.Typed) (signed descruntime.Signature, err error)
}

// ComponentSignatureVerifier validates signatures and digests for a Component Descriptor.
//
// Implementations MUST:
// - Expect that ALL contained digests were already precomputed from scratch for artifacts and component references BEFORE calling Verify.
// - Normalize the descriptor with the configured algorithm, then recompute the component-version digest.
// - Select signatures by name if provided; otherwise verify all present signatures.
// - Verify the cryptographic signature over the normalized digest using the provided configuration.
// - Return an error if any selected signature or required digest check fails.
// See: https://ocm.software/docs/reference/ocm-cli/verify/componentversions/
type ComponentSignatureVerifier interface {
    // Verify performs signature and digest checks using the provided config.
    // An extensible config SHOULD support:
    //   - signature name filters, normalization algorithm id,
    //   - key material (public keys, certificates, roots), issuer constraints,
    //   - verification time for certificate validity checks.
    Verify(ctx context.Context, descriptor descruntime.Descriptor, config runtime.Typed) error
}
```

---

## Example Configuration

### Signing

```yaml
signers:
  - name: myprivatekey
    type: OCMRSASignatureSigner/v1alpha1
    spec:
      normalization:
        algorithm: jsonNormalisation/v4alpha1
        hashAlgorithm: sha256
      algorithm: RSASSA-PKCS1-V1_5
      mediaType: application/vnd.ocm.signature.rsa
      privateKey:
        PEMFile: /path/to/myprivatekey.pem
```

### Verification

```yaml
verifiers:
  - name: mypublickey
    type: OCMRSASignatureVerifier/v1alpha1
    spec:
      normalization:
        algorithm: jsonNormalisation/v4alpha1
        hashAlgorithm: sha256
      publicKey:
        PEMFile: /path/to/mypublickey.pem
```

---

## Processing Architecture

1. **Input**

    * Descriptor reference or JSON/YAML.
    * Operation mode: sign or verify.
    * Resolved config from `.ocmconfig` or CLI flags.

2. **Normalization**

    * Apply configured normalization algorithm to the descriptor.
    * Recompute all artifact and reference digests.

3. **Digest Computation**

    * Compute the component-version digest over normalized bytes.

4. **Signing** *(sign mode)*

    * Produce signature envelope using selected algorithm and key.
    * Attach envelope to descriptor `signatures`.

5. **Verification** *(verify mode)*

    * Filter candidate signatures by name if provided.
    * Verify signature(s) against recomputed digest and trust material.

6. **Output**

    * Sign: descriptor with appended signature.
    * Verify: success or detailed error per failing signature.

---

## Pros and Cons

### Pros

* Consistent user experience for signing and verification.
* Pluggable algorithms and key formats.
* Decoupled from normalization internals.
* Testable contracts and clear error surfaces.

### Cons

* Requires plugin registry management.
* Implementors must understand normalization and digest rules.
* Risk of duplicated helpers if plugins ignore shared utilities.

---

## Conclusion

Adopt a unified, pluggable signing/verification contract around normalized component descriptors. This enforces spec compliance, preserves interoperability, and enables new cryptographic algorithms and trust models without changes to core CLI logic.
