# ADR: Monorepo Versioning, Branching & Release Strategy

* **Status:** proposed
* **Deciders:** OCM Technical Steering Committee
* **Date:** 2025‑09-30

---

## Technical Story

Enable fast, reliable releases from the new monorepo using Git‑native, tag‑only versioning and a simple branching model, while keeping a clean path to introduce an optional root component later.

### Context and Problem Statement

We have already consolidated two components into our monorepo

* cli - CLI v2
* kubernetes/controller - controller v2

and will add a third component later:

* website - OCM website

The team already operates GitHub‑based release workflows for OCM v1. We considered carrying over VERSION files, but decided to converge on **Git‑native, tag‑only** versioning for a single, immutable source of truth.

### Important scope decisions

* **Lockstep MAJOR/MINOR:** Both sub‑components use **the same X.Y** at sprint start. This is supportive for an `ocm` root component to be introduced later, which will share X.Y and patch in tandem when any sub-component patches.
* **Per‑component PATCH:** Patch releases may happen independently per sub‑component **between** sprints.
* **Promotion of RC to final** happens using the same artifacts produced by the RC build for the final release (no rebuild).

### Out of scope (for this ADR)

* **Emergency patches:** Any special treatment/definition of emergency patches; we define the process only for "normal" patches.
* **Support policy details beyond y‑2:** We set **y‑2** support (≈3 months) now; exact branch retirement/EOL steps will be defined later.
* **Testing strategy expansion:** Beyond current component‑specific tests; additional integration/system/conformance testing is excluded from this ADR.

---

## Decision Drivers

* Fast time‑to‑delivery from the monorepo.
* Git tags as single source of truth for versions.
* Compatibility with a future optional root component without reworking fundamentals.
* RC promotion workflow without rebuilds to fulfil SLSA recommendations.

---

## Options

### Version storage

* **V1 — VERSION files per component**

  * Each component stores its version in-tree (e.g., `cli/VERSION`, `kubernetes/cli/VERSION`).

* **V2 — Git tags only (tag‑only)**

  * Annotated tags per component are the single source of truth (e.g., `cli/vX.Y.Z[-rc.N]`).

### Branching strategy

* **B1 — Unified, single release branch per MINOR**

  * One branch for the repo per train: `releases/X.Y`. Every component releases from here; each receives its **own tags**, e.g., `cli/vX.Y.Z`, `kubernetes/controller/vX.Y.Z`.

* **B2 — Multiple release branches (one per component)**

  * `releases/cli/X.Y`, `releases/kubernetes/controller/X.Y` maintained in parallel, per‑component.

* **B3 — No permanent maintenance branches**

  * Patch on temporary branches checked out from final release tags; cherry‑pick fixes; delete after release.

---

## Decision Outcome

We will implement the combination of **V2 + B1** (Git tags only and a unified single release branch per MINOR). We will keep lockstep MAJOR/MINOR and allow per‑component PATCH between sprints.

**Why this combination?**

* Git tags as single SoT eliminate VERSION file/tag drift and reduce bump‑PR noise.
* Unified release branches `releases/X.Y` reduce operational complexity while preserving per‑component tags.
* Technical implementation not more difficult than option V2, but more future-proof and avoids later migration from V1 to V2
* An `ocm` root component can later be created from the same branch and tagged to match X.Y; root PATCH can be triggered in tandem when any sub‑component patches.

---

## Pros and Cons

### Version storage

**V2 — Git tags only (selected)**

* **Pros:** Single immutable SoT in Git; no bump PRs; cleaner history.
* **Cons:** Requires robust automation.

**V1 — VERSION files (not selected)**

* **Pros:** Familiar to the team; explicit in tree.
* **Cons:** Bump PR/commit noise; risk of file/tag drift; requires robust automation.

### Branching strategy

**B1 — Unified single branch per minor (selected)**

* **Pros:** Fewer branches; aligns with lockstep X.Y.
* **Cons:** Requires CI path filters on component level.

**B2 — Per‑component branches (not selected)**

* **Pros:** Isolation when components need divergent stabilization branches.
* **Cons:** More branches/backports; higher operational overhead; weaker alignment with lockstep cadence.

**B3 — No permanent maintenance branches (not selected)**

* **Pros:** Minimal long‑lived branches. Fully git‑native.
* **Cons:** Per‑patch bootstrapping; risk of using wrong base tag; needs strong automation.

---

## Implementation (Git tags for versioning and unified single release branch per MINOR)

### Tag namespaces

* `cli/vX.Y.Z[-rc.N]`
* `kubernetes/controller/vX.Y.Z[-rc.N]`

### Branching

`releases/X.Y` as the single staging lane for a MINOR release.

### CICD & Guardrails

**Tools:** GitHub Actions for all build and release workflows; protected branches/tags

#### Component-scoped tag discovery

Use `git describe` with `--match` to find the latest tag for a specific component, e.g., for `cli`:

```bash
git describe --tags --match "cli/v[0-9]*" 
```

#### Lockstep gates (MAJOR/MINOR)

* For major/minor RC and final: CI extracts the target X.Y for both components and fails if they differ.
* For patch releases: the gate applies only to the affected component (no lockstep check).

#### Preventing wrong tags

* Protect tag patterns: cli/v*, kubernetes/controller/v*.
* Deny manual pushes for those patterns (e.g. only allow OCM bot).

#### Release artifact model

* RC artifacts (images/binaries) are produced once and published with RC tags.
* Promotion without rebuild: final tags point to the same digest (OCI images) and the same binary checksums; no new build occurs.
* Provenance/Signatures: RCs are signed/attested (using [actions/attest-build-provenance](https://github.com/actions/attest-build-provenance)); promotion adds a final attestation referencing the same subject digest.

#### GPG-signed tags

All release tags (RC and final) are GPG-signed (`git tag -s`) to satisfy the OpenSSF Best Practices `version_tags_signed` criterion. The release workflows import a GPG key from org-level secrets (`GPG_PRIVATE_KEY_FOR_SIGNING`, `GPG_PASSPHRASE`) using [crazy-max/ghaction-import-gpg](https://github.com/crazy-max/ghaction-import-gpg) before creating any tag.

The corresponding public key is stored at `website/static/gpg/OCM-RELEASES-PUBLIC-CURRENT.gpg` and can be used to verify tags locally:

```bash
gpg --import website/static/gpg/OCM-RELEASES-PUBLIC-CURRENT.gpg
git tag -v <tag>
```

> **TODO:** Consider removing the key expiry (currently 2028-11-10) and relying on a stored revocation certificate instead. For smaller projects like OCM, a non-expiring key with a revocation certificate is simpler operationally and avoids user-facing breakage — e.g., Kubernetes' Nov 2024 incident where an expired package-signing key broke apt/yum installs for all users worldwide. The revocation certificate and private key are stored in the team password vault.

### Rollback & Immutability Policy

* Tags are immutable. Never delete or overwrite a published annotated tag.
* If a final release has a defect, ship a corrective patch (vX.Y.Z+1) and mark the previous final as superseded in the notes.
* RC defects found after promotion follow the same corrective-patch route.

### OCM components produced

Every release publishes three OCM component-versions to `ghcr.io/<owner>/component-descriptors/`, alongside the raw artifacts the GitHub release ships. This is the implementation of the originally out-of-scope "Root ocm component" item from this ADR.

* **`ocm.software/cli`** — six executable resources (linux/darwin/windows × amd64/arm64) embedded as local blobs plus the multi-arch CLI OCI image referenced by digest.
* **`ocm.software/controller`** — controller image and Helm chart, both referenced by digest.
* **`ocm.software/ocm`** — product wrapper; pure `componentReferences` to the two above. This is the canonical "what was released" handle.

All three carry the same bare semver as the GitHub release (`0.X.Y` final, `0.X.Y-rc.N` RC; no leading `v` — matches the OCI tags that `pipeline.yml` pushes for the underlying image and chart). They are published on **both** RC and final phases of the release workflow so that consumers can validate the component shape against an RC before a final lands.

**Conflict policy:** Both RC and final publications use `--component-version-conflict-policy replace`. This keeps reruns after a transient failure idempotent (mid-run flakes leave part of the three component-versions written; the rerun completes the rest without operator intervention). The release workflow's `concurrency` group and `create-tag.js`'s tag-existence checks prevent concurrent or diverging runs for the same version, so `replace` cannot silently overwrite a different commit's artifact.

**Resource form:**

* OCI artefacts (CLI image, controller image, Helm chart) are referenced **by digest** via `access.ociArtifact`. The image and chart are pushed earlier in the same release run; the component-version pins their digests, not their tags. Storage is not duplicated.
* CLI binaries are embedded **by value** via `input.type: file`. There is no per-platform OCI store for the six binaries, so digest-pinning is not available. This duplicates ~180 MB per release between the GitHub release assets and the component-version local blobs — an acceptable cost for offline transportability of the component-version.

**Constructor evolution:** within a single MAJOR, resources may be **added** to a component-version without breaking consumers (additive change). Removing a resource, renaming a `name`, or changing `extraIdentity` is a breaking change and requires a MAJOR bump. This invariant ties constructor evolution to the existing semver contract.

See [RELEASE_PROCESS.md § OCM components produced](../../RELEASE_PROCESS.md#ocm-components-produced) for consumer-facing pull commands.

### Roles and Responsibilities

* **Release Manager:** Orchestrates sprint releases, coordinates RC promotions, decides on emergency patches.  
* **Maintainers:** Ensure component readiness, validate release quality (together with PO).  
* **TSC (Technical Steering Committee):** Approves major changes to release process governance (e.g., changes to the release branching model, tag immutability policy, or support policy).

### Decision Gates

* **RC → Release Promotion:** Release Manager decision, with maintainer sign-off.
* **Patch Approval:** Release Manager decision
* **Major Policy Changes:** TSC approval (e.g., changes in support policy, release branching, or tag immutability).

### Example Workflow: Sprint Cycle and Orchestrated Release Day

#### Sprint N: Development Phase (2 weeks)

```bash
# Create release branch from main for new minor version
git checkout main
git checkout -b releases/v0.9
git push origin releases/v0.9

# Create RCs for all components on new release branch
cli/v0.9.0-rc.1
kubernetes/controller/v0.9.0-rc.1
```

#### Sprint N+1: RC Testing Phase (2 weeks)

```bash
# Bug found in CLI during RC testing
git checkout releases/v0.9
git cherry-pick <bugfix>    #from main

# Increment RC versions for affected components (trigger release workflow for new RC)
cli/v0.9.0-rc.2                     ← RC incremented due to patch
kubernetes/controller/v0.9.0-rc.1   ← unchanged
```

#### Sprint N+1 End: Orchestrated Release Day

**Release Manager orchestrates:** ALL current RCs get promoted to finals

```bash
# All RCs are promoted (no rebuilds)
cli/v0.9.0 
kubernetes/controller/v0.9.0

# Start next cycle - create new release branch + RCs
releases/v0.10 → cli/v0.10.0-rc.1, kubernetes/controller/v0.10.0-rc.1
```

#### Maintenance Patches During Sprints

```bash
# Security fix for previous cli release (already finalized)
git checkout releases/0.8  # previous release branch
git cherry-pick <security-fix>

# Create patch RCs for maintenance release
cli/v0.8.1-rc.1
kubernetes/controller/v0.8.0  ← keep same version if unaffected
```

#### Clarifications for Consistency

* PATCH version freedom vs. lockstep: It is intentional that `cli` can be at vX.Y.Z1 while controller remains at vX.Y.Z0 within the same X.Y line. MINORs are in lockstep; PATCHes are per component.
* RC period length: Default RC soak aligns with the sprint cadence. Maintenance patches may use a shorter RC period (risk-based), especially for security fixes.

### Release notes

* Per component, generated via path‑scoped diffs (e.g., `<root>/cli/**`, `<root>/kubernetes/controller/**`).
* Following same structure as in OCM v1 release, see [example](https://github.com/open-component-model/ocm/releases/tag/v0.30.0)

### Support Policy (y‑2 / ~3 months)

We will support the latest 3 MINOR versions (y, y‑1, y‑2). EOL activities for branches will be revisited later.

---
