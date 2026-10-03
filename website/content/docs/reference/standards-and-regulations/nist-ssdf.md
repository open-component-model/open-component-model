---
title: "NIST SSDF"
description: "Reference for OCM development practices mapped to NIST SP 800-218 (SSDF v1.1): practice groups PO, PS, PW, RV with repo evidence and gaps."
weight: 6
toc: true
---

This page maps the development practices of the OCM CLI and the OCM controller
to the Secure Software Development Framework (SSDF) defined in
[NIST SP 800-218 v1.1](https://csrc.nist.gov/pubs/sp/800/218/final) (February
2022). SP 800-218 Rev. 1 (SSDF v1.2) is in draft; this page covers the
published v1.1.

OCM does not give compliance guarantees. Whether a deployment satisfies a
regulatory requirement depends on your organization's policies, your risk
assessment, and controls outside OCM. This page documents what the project
provides as evidence so that you or your assessor can make that determination.

## Why This Matters

The SSDF is the technical foundation of the
[CISA Secure Software Development Attestation Form](https://www.cisa.gov/resources-tools/resources/secure-software-development-attestation-form),
which US federal agencies may require software producers to complete under OMB
Memorandum M-22-18 and its successor M-26-05. Software producers that sell to
the US federal government must attest that they follow SSDF practices. The
mapping below helps users of OCM understand which SSDF practices the project
already addresses and where gaps remain.

## Practice Groups

The SSDF defines four practice groups. The sections below list each practice,
the OCM evidence that supports it, and any gaps.

### PO — Prepare the Organization

| Practice | Name | OCM evidence | Gap |
| --- | --- | --- | --- |
| PO.1 | Define Security Requirements for Software Development | Security design principles in `docs/security/secure-design.md`; structured assurance case in `docs/security/assurance-case.md`; Architecture Decision Records in `docs/adr/` (ADR 0002 credentials, ADR 0008 signing). FIPS 140-3 and DISA STIG requirements documented in [FIPS 140-3]({{< relref "docs/reference/standards-and-regulations/fips.md" >}}) and [DISA STIG]({{< relref "docs/reference/standards-and-regulations/disa-stig.md" >}}). | No public document enumerating all security requirements in one place. |
| PO.2 | Implement Roles and Responsibilities | `.github/CODEOWNERS` assigns the Maintainers team as default reviewers and the TSC for `docs/steering` and `docs/adr`. The org-level security policy names the TSC as the security contact. | — |
| PO.3 | Implement Supporting Toolchains | CI workflow (`.github/workflows/ci.yml`) runs unit tests, integration tests, golangci-lint (with gosec), and code generation verification on every PR and push to `main`. Tool versions are pinned in `.env` files and managed by Renovate (`.github/renovate.json5`). | — |
| PO.4 | Define and Use Criteria for Software Security Checks | The pipeline (`.github/workflows/pipeline.yml`) gates publishing on conformance tests, end-to-end tests, and STIG scans: all three must pass before images are pushed to GHCR. CodeQL (`.github/workflows/codeql.yml`) runs daily with `security-extended` queries. OpenSSF Scorecard (`.github/workflows/openssf-scorecard.yml`) runs weekly and on push to `main`, publishing results to the OpenSSF REST API. | No documented minimum-score policy for Scorecard. |
| PO.5 | Implement and Maintain Secure Environments for Software Development | GitHub Actions runners use ephemeral environments. Workflows set `persist-credentials: false` on checkout steps. Actions are pinned to full SHA digests (enforced by Renovate `helpers:pinGitHubActionDigests`). zizmor (`.github/workflows/zizmor.yml`) scans all workflow files for security issues on every PR and push. | — |

### PS — Protect the Software

| Practice | Name | OCM evidence | Gap |
| --- | --- | --- | --- |
| PS.1 | Protect All Forms of Code from Unauthorized Access and Tampering | `.github/CODEOWNERS` requires Maintainers team review. DCO sign-off is mandatory on every commit (`AGENTS.md`, `CONTRIBUTING.md`). PR titles must follow Conventional Commits (enforced by `.github/workflows/pull-request.yaml`). Repository settings are synced from the org-level `.github` repo via Probot (`.github/settings.yml`). | Branch protection rules are configured at the GitHub level and not visible in the repository; cannot verify specific settings (e.g. required approvals, dismiss stale reviews) from repo content alone. |
| PS.2 | Provide a Mechanism for Verifying Software Release Integrity | Release tags are GPG-signed (`.github/workflows/release.yml`, `ghaction-import-gpg`). CLI binaries, CLI OCI image, controller image, and Helm chart all receive build-provenance attestations via `actions/attest-build-provenance` (`.github/workflows/cli.yml`, `.github/workflows/pipeline.yml`). Attestations are verified before promotion to a final release (`.github/workflows/release.yml`, `verify_attestations` job). Component versions are signed keyless with Sigstore (`.github/workflows/publish-components.yml`). | — |
| PS.3 | Archive and Protect Each Software Release | Releases are published to GitHub Releases with binaries and changelogs generated by git-cliff. OCI images and Helm charts are stored in GHCR with digest-pinned references. Release tags follow a defined scheme (`v0.X.Y`, `bindings/go/v0.X.Y`, `website/v0.X.Y`). | No documented retention or archival policy for older releases beyond the security policy's "latest 3 minors" support window. |

### PW — Produce Well-Secured Software

| Practice | Name | OCM evidence | Gap |
| --- | --- | --- | --- |
| PW.1 | Design Software to Meet Security Requirements and Mitigate Risks | `docs/security/secure-design.md` documents security design principles. `docs/security/assurance-case.md` maps threats to mitigations with evidence. ADRs record architectural decisions. Container images are built `FROM scratch` with a static binary and CA bundle only, no shell or package manager. | — |
| PW.2 | Review the Software Design | `.github/CODEOWNERS` requires review from the Maintainers team. ADRs in `docs/adr/` record design decisions for security-sensitive areas (credentials, signing). | — |
| PW.4 | Reuse Existing, Well-Secured Software | OCM uses the Go standard library's Go Cryptographic Module for all internal cryptography instead of maintaining its own. Dependencies are tracked in `bindings/go/go.mod` and updated by Renovate with a 28-day minimum release age. | — |
| PW.5 | Create Source Code by Adhering to Secure Coding Practices | golangci-lint with gosec runs on every PR (`.github/workflows/ci.yml`). The shared `golangci.yml` enables `default: all` linters. `CONTRIBUTING.md` and area-specific contributing guides document coding conventions. | — |
| PW.6 | Configure Compilation, Interpreter, and Build Processes to Improve Executable Security | Binaries are built with `CGO_ENABLED=0` (static linking), `GOFIPS140=certified`, and `-trimpath -ldflags="-s -w"`. The build configuration is defined in Taskfiles sourced from `.env`, not hard-coded in scripts. | — |
| PW.7 | Review and/or Analyze Human-Readable Code | CodeQL with `security-extended` queries runs daily on Go code (`.github/workflows/codeql.yml`). golangci-lint with gosec runs per-PR. zizmor scans GitHub Actions workflow files (`.github/workflows/zizmor.yml`). | — |
| PW.8 | Test Executable Code | Unit tests and integration tests run on every PR (`.github/workflows/ci.yml`). Coverage thresholds are enforced (unit floor 62%, integration floor 43%). Conformance and end-to-end tests gate the release pipeline (`.github/workflows/pipeline.yml`). STIG scans run on built images before publishing. | No fuzzing or dynamic application security testing (DAST) in CI. |
| PW.9 | Configure Software to Have Secure Settings by Default | FIPS mode is on by default (`fips140=on`). Container images run as non-root user 65532 with all capabilities dropped, read-only root filesystem, and `seccompProfile: RuntimeDefault`. The controller chart meets the Kubernetes restricted Pod Security Standard. See [FIPS 140-3]({{< relref "docs/reference/standards-and-regulations/fips.md" >}}) and [DISA STIG]({{< relref "docs/reference/standards-and-regulations/disa-stig.md" >}}). | — |

### RV — Respond to Vulnerabilities

| Practice | Name | OCM evidence | Gap |
| --- | --- | --- | --- |
| RV.1 | Identify and Confirm Vulnerabilities on an Ongoing Basis | CodeQL runs daily (`.github/workflows/codeql.yml`). `govulncheck` runs in source mode on every Go code change (`.github/workflows/ci.yml`), gated by the required CI completion check, providing reachability-aware vulnerability analysis. Every container image is scanned with Trivy using an OpenVEX document from `govulncheck` (`.github/workflows/image-scan.yml`); the scan gates publishing and reports only reachable vulnerabilities. OpenSSF Scorecard runs weekly (`.github/workflows/openssf-scorecard.yml`). Renovate creates dependency update PRs with a 28-day minimum release age and groups for fast-moving dependencies. The org-level [security policy](https://github.com/open-component-model/.github/blob/main/SECURITY.md) accepts vulnerability reports through GitHub private vulnerability reporting or email to the TSC. SPDX JSON SBOMs are generated with Syft for every CLI binary and OCI image and attested with `actions/attest`. OpenVEX documents are attested to each OCI image. | — |
| RV.2 | Assess, Prioritize, and Remediate Vulnerabilities | The security policy defines severity-based response targets (Critical ≤ 14 days fix, High ≤ 30 days, Medium ≤ 90 days). Published advisories are listed in each repository's Security → Advisories tab. | — |
| RV.3 | Analyze Vulnerabilities to Identify Their Root Causes | ADRs document architectural decisions and their security rationale. The assurance case (`docs/security/assurance-case.md`) maps threats to mitigations. | No formal root-cause analysis process is documented. |

## Summary of Gaps

| Gap | SSDF practice | Notes |
| --- | --- | --- |
| No fuzzing or DAST | PW.8 | Static analysis (CodeQL, gosec) and unit/integration/conformance/e2e tests are in place, but no fuzz testing or dynamic analysis runs in CI. |
| No formal root-cause analysis process | RV.3 | ADRs and the assurance case exist, but no documented process requires root-cause analysis after a vulnerability fix. |
| No documented minimum Scorecard policy | PO.4 | The OpenSSF Scorecard runs and publishes results, but no minimum score is enforced as a gate. |
| Branch protection not verifiable from repo | PS.1 | GitHub branch protection settings are not committed to the repository. |

## Further Reading

- [NIST SP 800-218 v1.1](https://csrc.nist.gov/pubs/sp/800/218/final) — the
  published SSDF.
- [SP 800-218 Rev. 1 (draft)](https://csrc.nist.gov/pubs/sp/800/218/r1/ipd) —
  SSDF v1.2, adding practices PO.6 (continuous improvement) and PS.4 (robust
  updates). Initial public draft December 2025; not yet finalized.
- [CISA Secure Software Development Attestation Form](https://www.cisa.gov/resources-tools/resources/secure-software-development-attestation-form) —
  the form federal agencies may require from software producers.
- [SLSA]({{< relref "docs/reference/standards-and-regulations/slsa.md" >}}) —
  build provenance and supply-chain integrity levels.
- [FIPS 140-3]({{< relref "docs/reference/standards-and-regulations/fips.md" >}}) —
  cryptographic module usage in OCM.
- [DISA STIG]({{< relref "docs/reference/standards-and-regulations/disa-stig.md" >}}) —
  container hardening and GPOS SRG scan.
