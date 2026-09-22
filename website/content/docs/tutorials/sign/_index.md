---
title: "Sign"
description: "Tutorials for signing and verifying OCM component versions."
weight: 20
sidebar:
  collapsed: false
---

During the sign phase you attach a cryptographic proof to a component version. These tutorials
walk through each trust model OCM supports end-to-end, so you can choose the one that fits your
key management setup.

- [Plain Signatures]({{< relref "docs/tutorials/sign/plain.md" >}}) — sign and verify with a raw RSA key pair
- [Certificate Chains (PEM)]({{< relref "docs/tutorials/sign/pem.md" >}}) — sign with a private key and verify against a certificate chain
- [GPG Signatures]({{< relref "docs/tutorials/sign/gpg.md" >}}) — sign and verify using a GPG key pair
- [Sigstore (Keyless)]({{< relref "docs/tutorials/sign/sigstore.md" >}}) — sign without managing keys using the Sigstore keyless flow

For a side-by-side comparison of the trust models, see
[Signing and Verification — Trust Models]({{< relref "docs/concepts/sign/signing-and-verification-concept.md#trust-models" >}}).
