---
title: "Signing and Verification"
description: "Tutorials covering cryptographic signing and verification of OCM component versions, from basic key-pair signing to PKI-based certificate chains."
icon: "✍️"
weight: 55
toc: false
---

OCM supports three signing approaches. Pick the tutorial that matches the trust model you want to use.

| Tutorial | Algorithm | Trust anchor | When to choose it |
| --- | --- | --- | --- |
| [Plain Signatures]({{< relref "plain.md" >}}) | RSA key pair | Public key the verifier holds | Small teams, self-signed workflows, no PKI |
| [Certificate Chains (PEM)]({{< relref "pem.md" >}}) | RSA + X.509 chain | Root CA the verifier holds | Existing PKI, organizational delegation, key rotation without verifier reconfiguration |
| [Sigstore (Keyless)]({{< relref "sigstore.md" >}}) | Sigstore (ECDSA, ephemeral) | OIDC identity the verifier trusts | Skip key management entirely; built-in audit trail via the Rekor transparency log |

For the conceptual background and a side-by-side comparison of the three trust models, see [Concept: Signing and Verification — Trust Models]({{< relref "docs/concepts/signing-and-verification-concept.md#trust-models" >}}).
