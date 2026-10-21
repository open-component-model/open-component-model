---
title: "Sign"
description: "Sign and verify component versions so consumers can trust their authenticity and integrity."
icon: "🔏"
weight: 20
aliases:
  - /docs/tutorials/signing/
  - /docs/how-to/sign-and-verify/
  - /docs/getting-started/sign-component-versions/
  - /docs/how-to/sign-and-verify/sign-component-version/
  - /docs/how-to/sign-and-verify/sign-component-versions/
  - /docs/reference/ocm-cli/verify/componentversions/
  - /docs/how-to/sign-and-verify/verify-component-version/
  - /docs/how-to/sign-and-verify/verify-component-versions/
sidebar:
  collapsed: true
---

Signing certifies that a component version is authentic and has not been tampered with. These guides walk you through generating keys, configuring signing credentials, and both sides of each method: a sign guide for producers and a matching verify guide for consumers (plain RSA, PEM certificate chains, GPG, and keyless Sigstore).

## Verify a signature

To validate a component version signature, pick the method that matches the algorithm the signature was made with. Each page is a self-contained walkthrough.

| Method | When to use it | Sign | Verify |
| --- | --- | --- | --- |
| **Plain RSA** | An RSA key pair; the consumer verifies with the signer's public key on disk. | [Sign with Plain RSA]({{< relref "docs/guides/sign/sign-with-plain-rsa.md" >}}) | [Verify Plain RSA]({{< relref "docs/guides/sign/verify-with-plain-rsa.md" >}}) |
| **PEM certificate chain** | An X.509 chain embedded in the signature; the consumer pins the signer's root CA. | [Sign with a PEM Certificate Chain]({{< relref "docs/guides/sign/sign-with-pem-certificate-chain.md" >}}) | [Verify a PEM Certificate Chain]({{< relref "docs/guides/sign/verify-with-pem-certificate-chain.md" >}}) |
| **GPG** | A GPG (OpenPGP) key; the consumer verifies with the signer's public key or keyring. | [Sign with GPG]({{< relref "docs/guides/sign/sign-with-gpg.md" >}}) | [Verify GPG]({{< relref "docs/guides/sign/verify-with-gpg.md" >}}) |
| **Sigstore (keyless)** | No key pair; the consumer declares which OIDC identity to trust, interactively or in CI. | [Sign with Sigstore (Keyless)]({{< relref "docs/guides/sign/sign-with-sigstore.md" >}}) | [Verify Sigstore (Keyless)]({{< relref "docs/guides/sign/verify-with-sigstore.md" >}}) |

Not sure which algorithm was used? List the signatures on the component and read the `algorithm` field:

```bash
ocm get cv <repository>//<component>:<version> -o yaml | grep -A 10 signatures:
```

- `RSASSA-PSS` (default) or `RSASSA-PKCS1-V1_5`, mediaType `application/vnd.ocm.signature.rsa.pss` → [Verify Plain RSA]({{< relref "docs/guides/sign/verify-with-plain-rsa.md" >}})
- a PEM-encoded signature, mediaType `application/x-pem-file` → [Verify a PEM Certificate Chain]({{< relref "docs/guides/sign/verify-with-pem-certificate-chain.md" >}})
- an OpenPGP signature → [Verify GPG]({{< relref "docs/guides/sign/verify-with-gpg.md" >}})
- `Sigstore/v1alpha1` → [Verify Sigstore (Keyless)]({{< relref "docs/guides/sign/verify-with-sigstore.md" >}})
