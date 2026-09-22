---
title: "Sign"
description: "How-to guides for cryptographically signing and verifying OCM component versions."
weight: 20
sidebar:
  collapsed: false
---

During the sign phase you attach a cryptographic proof to a component version so consumers can
verify its integrity after transfer. These guides cover the full signing workflow with the OCM CLI.

- [Generate Signing Keys]({{< relref "docs/how-to/sign/generate-signing-keys.md" >}}) — create RSA or GPG key pairs for signing
- [Configure Credentials for Signing]({{< relref "docs/how-to/sign/configure-signing-credentials.md" >}}) — set up key references in the OCM config
- [Sign Component Versions]({{< relref "docs/how-to/sign/sign-component-version.md" >}}) — apply a signature to a component version in a registry
- [Verify Component Versions]({{< relref "docs/how-to/sign/verify-component-version.md" >}}) — check a signature against a trusted public key or certificate
