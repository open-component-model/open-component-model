---
title: "Standards & Regulations"
description: "How the OCM CLI and OCM controller relate to security standards and regulations, starting with FIPS 140-3 cryptography."
icon: "🛡️"
weight: 100
toc: true
sidebar:
  collapsed: true
---

This section describes how the OCM CLI and the OCM controller relate to security
standards and regulations. OCM does not give compliance guarantees: whether a
deployment meets a standard depends on more than OCM.

- [FIPS 140-3]({{< relref "docs/reference/standards-and-regulations/fips.md" >}}):
  the Go Cryptographic Module in the OCM binaries, runtime modes, digest
  algorithms, and external signing binaries (`cosign`, `gpg`).
