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
  - /docs/reference/ocm-cli/verify/componentversions/
  - /docs/how-to/sign-and-verify/verify-component-version/
sidebar:
  collapsed: true
---

Signing certifies that a component version is authentic and has not been tampered with. These guides walk you through generating keys, configuring signing credentials, signing and verifying component versions, and using specific signing methods (plain RSA, PEM certificate chains, GPG, and keyless Sigstore).

## Verify a signature

To validate a component version signature, pick the method that matches the algorithm the signature was made with. Each page is a self-contained walkthrough.

| Method | When to use it | Guide |
| --- | --- | --- |
| **Plain RSA** | The signature was made with an RSA key and you have the signer's public key on disk. | [Verify an RSA signature]({{< relref "docs/guides/sign/sign-with-plain-rsa.md#verify-an-rsa-signature" >}}) |
| **GPG** | The signature was made with a GPG (OpenPGP) key and you have the signer's public key or keyring. | [Verify a GPG signature]({{< relref "docs/guides/sign/sign-with-gpg.md#verify-a-gpg-signature" >}}) |
| **Sigstore (keyless)** | The signature was made keyless with Sigstore, either interactively or in CI. You declare which OIDC identity you trust instead of installing a public key. | [Verify a Sigstore signature]({{< relref "docs/guides/sign/sign-with-sigstore.md#verify-a-sigstore-signature" >}}) |

Not sure which algorithm was used? List the signatures on the component and read the `algorithm` field:

```bash
ocm get cv <repository>//<component>:<version> -o yaml | grep -A 10 signatures:
```

- `RSASSA-PSS` (default) or `RSASSA-PKCS1-V1_5`, mediaType `application/vnd.ocm.signature.rsa.pss` → [Verify an RSA signature]({{< relref "docs/guides/sign/sign-with-plain-rsa.md#verify-an-rsa-signature" >}})
- an OpenPGP signature → [Verify a GPG signature]({{< relref "docs/guides/sign/sign-with-gpg.md#verify-a-gpg-signature" >}})
- `Sigstore/v1alpha1` → [Verify a Sigstore signature]({{< relref "docs/guides/sign/sign-with-sigstore.md#verify-a-sigstore-signature" >}})
