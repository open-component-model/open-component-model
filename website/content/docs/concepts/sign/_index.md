---
title: "Sign"
description: "Understand how OCM secures component integrity and authenticity through cryptographic signatures."
weight: 20
sidebar:
  collapsed: false
---

Signing is the second step in the OCM lifecycle: it attaches a cryptographic proof to a component
version so that any consumer can verify its integrity and origin after transfer. OCM stores
signatures inside the component descriptor so they travel with the component unchanged, and
supports multiple trust models to match different key management practices.

- [Signing and Verification]({{< relref "docs/concepts/sign/signing-and-verification-concept.md" >}}) — signature structure, normalisation, trust models (RSA, GPG, certificate chains, Sigstore), and verification semantics
