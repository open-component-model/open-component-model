---
title: "Standards & Regulations"
description: "How the OCM CLI and OCM controller relate to security standards and regulations: FIPS 140-3, DISA STIG, EU CRA, BSI, CIS, NIST SSDF and SLSA."
icon: "🛡️"
weight: 100
toc: true
sidebar:
  collapsed: true
---

This section describes how the OCM CLI and the OCM controller relate to security
standards and regulations. OCM does not give compliance guarantees: whether a
deployment meets a standard depends on more than OCM.

## FIPS 140-3 and DISA STIG

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

## Further Standards and Regulations

| Page | Covers |
| --- | --- |
| [EU Cyber Resilience Act]({{< relref "docs/reference/standards-and-regulations/cra.md" >}}) | Regulation (EU) 2024/2847: vulnerability handling, security updates, SBOMs |
| [BSI IT-Grundschutz]({{< relref "docs/reference/standards-and-regulations/bsi-it-grundschutz.md" >}}) | BSI modules SYS.1.6 (containers) and APP.4.4 (Kubernetes) |
| [Security Benchmarks]({{< relref "docs/reference/standards-and-regulations/benchmarks.md" >}}) | CIS Docker and Kubernetes Benchmarks, NSA/CISA Kubernetes Hardening Guide |
| [NIST SSDF]({{< relref "docs/reference/standards-and-regulations/nist-ssdf.md" >}}) | NIST SP 800-218 secure software development practices |
| [SLSA]({{< relref "docs/reference/standards-and-regulations/slsa.md" >}}) | Supply-chain Levels for Software Artifacts, build provenance |
