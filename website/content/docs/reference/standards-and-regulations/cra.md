---
title: "EU Cyber Resilience Act"
description: "How OCM relates to the EU Cyber Resilience Act (CRA): open-source stewardship, essential requirements, vulnerability handling, and remaining gaps."
weight: 3
toc: true
---

This page describes how the Open Component Model (OCM) project relates to
[Regulation (EU) 2024/2847](https://eur-lex.europa.eu/eli/reg/2024/2847/oj/eng),
the EU Cyber Resilience Act (CRA). It covers the project's role under the CRA,
maps the regulation's essential requirements and vulnerability-handling
obligations to what OCM has today, and lists gaps.

This page is not legal advice. Whether the CRA applies to your product, and
which obligations fall on you as a manufacturer, importer, or distributor,
depends on your specific circumstances. Consult legal counsel for your situation.

## What the CRA Is

The CRA is a horizontal EU regulation that sets cybersecurity requirements for
hardware and software products ("products with digital elements") placed on the
EU market. It entered into force on 10 December 2024. Key dates:

| Milestone | Date |
| --- | --- |
| Entry into force | 10 December 2024 |
| Conformity assessment body notification (Chapter IV) | 11 June 2026 |
| Vulnerability and incident reporting obligations (Article 14) | 11 September 2026 |
| Full application of all obligations | 11 December 2027 |

Source:
[European Commission CRA policy page](https://digital-strategy.ec.europa.eu/en/policies/cyber-resilience-act),
[EUR-Lex legislative summary](https://eur-lex.europa.eu/legal-content/EN/LSU?uri=CELEX%3A32024R2847).

## OCM's Role under the CRA

The CRA distinguishes between **manufacturers** (who place products on the EU
market) and **open-source software stewards** (legal persons that systematically
support the development of free and open-source software intended for commercial
activities). Stewards have lighter obligations focused on cybersecurity policy,
vulnerability handling, and reporting; they are not subject to penalties for
CRA infringements (Article 24, Recital 25).

OCM is an open standard and its reference implementation. The project is
governed by the
[NeoNephos Foundation](https://neonephos.org/), a project community under the
[Linux Foundation](https://www.linuxfoundation.org/). The Linux Foundation acts
as CRA steward for its hosted projects and is registered on the single
reporting platform of ENISA for regulatory notifications
([NeoNephos Security Guidelines §11](https://github.com/neonephos/guidelines-development/blob/main/security-guidelines/security-guidelines.md#11-eu-cyber-resilience-act-cra-compliance)).
The steward contact is `steward@linuxfoundation.org`.

**If you integrate OCM into a product you place on the EU market, you are
likely the manufacturer under the CRA.** The manufacturer obligations (Annex I,
Article 13, Article 14) are yours. OCM as an open-source project supports your
compliance through the security practices described below, but does not give
compliance guarantees.

For background on how the Linux Foundation approaches CRA stewardship for open
source, see
[LF Guidance: CRA Introduction](https://bestpractices.linuxfoundation.org/regulatory/cra/introduction.html).

## Essential Requirements (Annex I, Part I)

Annex I, Part I lists security properties that manufacturers must ensure in
their products. The table below maps selected requirements to what OCM provides
today and where gaps remain. Requirements are paraphrased; the
[full text is on EUR-Lex](https://eur-lex.europa.eu/legal-content/EN/TXT/HTML/?uri=OJ:L_202402847#anx_I).

| Requirement (paraphrased) | OCM today | Gap |
| --- | --- | --- |
| Products designed, developed, produced with appropriate cybersecurity | Security design documented in [`docs/security/secure-design.md`](https://github.com/open-component-model/open-component-model/blob/main/docs/security/secure-design.md); structured threat model and assurance case in [`docs/security/assurance-case.md`](https://github.com/open-component-model/open-component-model/blob/main/docs/security/assurance-case.md). | These are contributor/auditor documents, not formal CRA technical documentation. |
| Delivered without known exploitable vulnerabilities | CodeQL SAST runs daily on the default branch with `security-extended` queries (`.github/workflows/codeql.yml`). golangci-lint with gosec runs per PR. Renovate updates dependencies daily and reads Dependabot vulnerability alerts to prioritize security updates (`.github/workflows/renovate.yml`). | No container image vulnerability scanning (e.g., Trivy, Grype) in the release pipeline. |
| Secure by default configuration | FIPS 140-3 mode is on by default (`fips140=on`). Controller pod spec meets the Kubernetes restricted Pod Security Standard. Both images run as non-root user 65532 from `scratch` with no shell, no package manager, all capabilities dropped. See [FIPS 140-3]({{< relref "docs/reference/standards-and-regulations/fips.md" >}}) and [DISA STIG]({{< relref "docs/reference/standards-and-regulations/disa-stig.md" >}}). | — |
| Protection against unauthorized access; access control | The controller chart enforces `readOnlyRootFilesystem`, `runAsNonRoot`, `seccompProfile: RuntimeDefault`, and drops all capabilities. Credential resolution is separated from signing configuration ([ADR-0002](https://github.com/open-component-model/open-component-model/blob/main/docs/adr/0002_credentials.md)). | Access control for multi-tenant use is the platform operator's responsibility. |
| Protect confidentiality and integrity of data | All cryptography in OCM runs through the Go Cryptographic Module ([CMVP #5247](https://csrc.nist.gov/projects/cryptographic-module-validation-program/certificate/5247)). TLS restricted to FIPS-approved cipher suites in FIPS mode. Component versions can be signed (RSA, Sigstore) and verified. See [FIPS 140-3]({{< relref "docs/reference/standards-and-regulations/fips.md" >}}). | — |
| Minimize attack surface | `scratch` images contain only the static binary and a CA bundle. No shell, dynamic linker, package manager, or setuid/setgid binaries. The DISA GPOS SRG scan enforces this at release time. See [DISA STIG]({{< relref "docs/reference/standards-and-regulations/disa-stig.md" >}}). | — |
| Software bill of materials (SBOM) | OCM can transport SBOMs as component resources (CycloneDX, SPDX) alongside the artifacts they describe, as explored in a [proof-of-concept](https://ocm.software/blog/2026-07-28-shipping-sboms-with-your-components). | OCM does not yet generate SBOMs for its own release artifacts (CLI binaries, container images, Helm chart). No SBOM generation step in the release pipeline. |

## Vulnerability Handling (Annex I, Part II)

Annex I, Part II requires manufacturers to handle vulnerabilities effectively
for the product's support period. As an open-source steward project, OCM
provides the upstream vulnerability handling that manufacturers can build on.

| Obligation (paraphrased) | OCM today | Gap |
| --- | --- | --- |
| Identify and document vulnerabilities, including in third-party components | Renovate monitors all direct and transitive Go module, GitHub Actions, and npm dependencies for known vulnerabilities (`.github/workflows/renovate.yml`). CodeQL (`security-extended`) runs daily. golangci-lint with gosec runs per PR. | No dedicated `govulncheck` integration. No container image scanning. |
| Receive and process vulnerability reports | Organization-level [security policy](https://github.com/open-component-model/.github/blob/main/SECURITY.md) provides GitHub private vulnerability reporting (preferred) and an email fallback (`open-component-model-tsc@lists.neonephos.org`). The TSC serves as security contact. Initial response within 14 calendar days per the [NeoNephos Security Guidelines §6.1](https://github.com/neonephos/guidelines-development/blob/main/security-guidelines/security-guidelines.md#61-initial-response). | — |
| Apply fixes, provide security updates without delay | Three latest minor releases receive security fixes. Severity-based fix targets from critical (≤ 14 days) to low (best effort). Security fixes are released as patch versions for all supported branches. See the [security policy](https://github.com/open-component-model/.github/blob/main/SECURITY.md). | — |
| Disclose vulnerabilities, CVE identifiers, corrective measures | Coordinated disclosure with 90-day embargo ceiling. Advisories published via GitHub Security Advisories with CVE identifiers for CVSS ≥ 4.0. Advisories are referenced in release notes of patched versions. | — |
| Ensure security updates are distributed free of charge; inform users | Releases are published on GitHub (source, binaries) and GHCR (container images, Helm chart). All artifacts are free. | No structured notification mechanism beyond release notes and GitHub advisories. |
| Establish a coordinated vulnerability disclosure policy | The security policy describes the full process: private reporting, triage, severity classification (CVSS v3.1+), fix, advisory. Follows [NeoNephos Security Guidelines](https://github.com/neonephos/guidelines-development/blob/main/security-guidelines/security-guidelines.md). | — |

## Reporting under Article 14

From 11 September 2026, manufacturers must report actively exploited
vulnerabilities and severe incidents to the CSIRT of their Member State and to
ENISA within 24 hours (early warning), 72 hours (main notification), and 14
days / 1 month (final report). These reports go through the
[ENISA CRA Single Reporting Platform](https://reporting.enisa.europa.eu).

OCM's steward, the Linux Foundation, handles regulatory reporting for
NeoNephos projects. Projects that become aware of an actively exploited
vulnerability or a severe incident (e.g., CI/CD compromise) must notify
`steward@linuxfoundation.org` within 24 hours. The steward then coordinates the
regulatory notification on behalf of the project
([NeoNephos Security Guidelines §11.2](https://github.com/neonephos/guidelines-development/blob/main/security-guidelines/security-guidelines.md#112-escalation-for-actively-exploited-vulnerabilities-and-severe-incidents)).

**If you are a manufacturer**, the Article 14 reporting obligations are yours.
OCM's upstream vulnerability handling (security policy, advisories, fix
releases) gives you the information you need, but the regulatory reports are
your responsibility.

## Supply Chain Security Measures

These measures are not CRA requirements on stewards, but they support
manufacturers building on OCM.

| Measure | OCM today | Gap |
| --- | --- | --- |
| Build provenance / SLSA attestation | `actions/attest-build-provenance` (SLSA) attests CLI binaries (`.github/workflows/cli.yml`), OCI images and Helm charts (`.github/workflows/pipeline.yml`, `.github/workflows/release.yml`). Attestations are published alongside artifacts. | — |
| Component version signing | Release component versions are signed keyless with Sigstore (Fulcio + Rekor) in the publish-components workflow (`.github/workflows/publish-components.yml`). Git tags are GPG-signed (`.github/workflows/release.yml`). | — |
| SBOM generation | — | OCM does not generate SBOMs for its own releases. BSI [TR-03183 Part 2](https://www.bsi.bund.de/EN/Themen/Unternehmen-und-Organisationen/Standards-und-Zertifizierung/Technische-Richtlinien/TR-nach-Thema-sortiert/tr03183/tr-03183.html) provides technical guidance on SBOM content for CRA compliance. |
| CSAF / VEX advisories | — | OCM publishes GitHub Security Advisories but does not produce machine-readable CSAF or VEX documents. BSI [TR-03183 Part 3](https://www.bsi.bund.de/EN/Themen/Unternehmen-und-Organisationen/Standards-und-Zertifizierung/Technische-Richtlinien/TR-nach-Thema-sortiert/tr03183/tr-03183.html) describes vulnerability report and notification formats. |
| OpenSSF Scorecard | Runs weekly and on push to `main` via `ossf/scorecard-action` (`.github/workflows/openssf-scorecard.yml`). Results are published to the OpenSSF API and uploaded to GitHub code scanning. | — |
| Cryptographic hardening | FIPS 140-3 mode on by default. See [FIPS 140-3]({{< relref "docs/reference/standards-and-regulations/fips.md" >}}). | — |
| Container and chart hardening | `scratch` images, DISA GPOS SRG scan, restricted pod security. See [DISA STIG]({{< relref "docs/reference/standards-and-regulations/disa-stig.md" >}}). | — |

## Summary of Gaps

| Gap | Impact |
| --- | --- |
| No SBOM generation for OCM's own release artifacts | Manufacturers integrating OCM cannot source an authoritative SBOM from the project; they must generate their own. |
| No CSAF or VEX output | Vulnerability information is human-readable (GitHub Advisories) but not machine-readable in standard exchange formats. |
| No container image vulnerability scanning in the release pipeline | Known vulnerabilities in base layers or Go toolchain would not be caught before release, though the `scratch` images have no OS packages. |
| No `govulncheck` integration | Reachability-aware Go vulnerability analysis is not run in CI. Renovate covers known CVEs in dependencies; CodeQL covers code-level issues. |
| Security design documents are not formal CRA technical documentation | The documents exist and are thorough, but they are not structured as CRA Annex VII technical documentation. This is primarily a manufacturer obligation. |

## Further Reading

- [Regulation (EU) 2024/2847 full text](https://eur-lex.europa.eu/eli/reg/2024/2847/oj/eng) (EUR-Lex)
- [European Commission CRA summary](https://digital-strategy.ec.europa.eu/en/policies/cra-summary)
- [LF Guidance: CRA Introduction](https://bestpractices.linuxfoundation.org/regulatory/cra/introduction.html)
- [NeoNephos Security Guidelines](https://github.com/neonephos/guidelines-development/blob/main/security-guidelines/security-guidelines.md)
- [OCM Security Policy](https://github.com/open-component-model/.github/blob/main/SECURITY.md)
- [BSI TR-03183: Cyber Resilience Requirements for Manufacturers and Products](https://www.bsi.bund.de/EN/Themen/Unternehmen-und-Organisationen/Standards-und-Zertifizierung/Technische-Richtlinien/TR-nach-Thema-sortiert/tr03183/tr-03183.html)
