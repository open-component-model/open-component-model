---
title: "Standards & Regulations"
description: "How the OCM CLI and the OCM controller support FIPS 140-3 and the DISA STIG, and how the two standards relate to each other."
icon: "🛡️"
weight: 100
toc: true
sidebar:
  collapsed: true
---

FIPS 140-3 and the DISA STIG are complementary cybersecurity standards. FIPS
140-3 validates cryptographic modules, while the DISA STIG provides holistic
system-hardening configuration baselines that mandate those validated modules.

| Feature | FIPS 140-3 | DISA STIG |
| --- | --- | --- |
| Focus | Cryptography | Complete system hardening |
| Governing body | NIST (National Institute of Standards and Technology) | DISA (Defense Information Systems Agency) |
| Type of standard | Cryptographic module validation and testing | Configuration requirements and technical controls |
| Scope | Algorithms, keys, and boundary protection | Operating systems, applications, and their configuration |

- [FIPS 140-3]({{< relref "docs/reference/standards-and-regulations/fips.md" >}}):
  the Go Cryptographic Module in the OCM binaries, runtime modes, digest
  algorithms, and external signing binaries (`cosign`, `gpg`).
- [DISA STIG]({{< relref "docs/reference/standards-and-regulations/disa-stig.md" >}}):
  container image and chart hardening, and the GPOS SRG scan of the OCM images.
