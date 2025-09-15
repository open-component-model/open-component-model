# ADR 0009 — Versioning and Release Branching for the OCM Monorepo

* Status: Proposed
* Deciders: OCM Technical Steering Committee
* Date: 2025-09-15

## Context

The repository contains multiple independently developed components, e.g., `ocm-cli`, `ocm-controller`, and a to-be-created root/wrapper component `ocm` (final name to be decided. Acts as a root component referencing versions of the sub-components).

We require a single, operational specification that defines how components are versioned, how release branches are created and maintained, and how CI publishes releases.

Architectural requirement: deciding on a versioning strategy must include the implications for branching. Without an agreed branching model the versioning choice is incomplete and cannot be implemented safely.

## Decision Drivers

* Reproducible, auditable published releases.
* Maintain per-component autonomy where practical.
* Support simple and robust hotfix and backport workflows for older minor releases.
* Provide a machine-readable snapshot for documentation and website consumption.
* Keep CI complexity manageable and deterministic.

## Proposal

We adopt a combined, opinionated strategy:

* Versioning: Hybrid approach — each component maintains a local `VERSION` file; the repository also maintains a generated `/VERSIONS.yaml` that mirrors the last published versions of all components (aka snapshot)
* Branching: Component-scoped persistent release branches — branches follow the pattern `releases/<component>/v<major>.<minor>` and act as maintenance lines for each active minor release. Patches and hotfixes are applied via PRs to these branches.
* Publishing: An annotated tag `<component>/v<major>.<minor>.<patch>` is the authoritative record of a published artifact.

## SemVer Principles for Root Component

### Normal Case (Automatic)

The `ocm` root component version follows these automated rules for independent sub-component releases:

* **Patch bump (x.y.z → z++)**: triggered by any sub-component patch release
* **Minor bump (x.y.z → y++)**: triggered by any sub-component feature addition  
* **Major bump (x.y.z → x++)**: triggered by any sub-component new major release

Every OCM root release represents a reproducible snapshot of all sub-component versions and maintains strict SemVer compliance. The root component version is automatically computed and updated in `/VERSIONS.yaml` when any sub-component publishes a release.

### Edge Case: Coordinated Multi-Component Releases

When multiple sub-components must be released together to solve a single problem:

**Problem:** Automatic root bumping would create multiple OCM releases instead of one logical `ocm`release containing all related changes.

**Solution:** Manual override workflow for coordinated releases:

1. **Sub-component releases with root suppression:**

   Execute the GitHub Action release workflow for each affected sub-component with the "suppress-root-release" flag enabled (flag name to be finalized).

   * Run release workflow for `ocm-cli` with flag: `suppress-root-release = true`
   * Run release workflow for `ocm-controller` with flag: `suppress-root-release = true`
   * Each workflow completes its normal release process (tag creation, artifact publishing) but skips the automatic OCM root release trigger.

2. **Manual root release consolidation:**

   After all coordinated sub-component releases are completed, manually trigger the "Create OCM Root Release" workflow:

   * Reads current `/VERSIONS.yaml` state to identify all unreleased changes
   * Computes appropriate bump type (highest sub-component bump type across all coordinated releases)
   * Creates single OCM release tag containing all coordinated changes

**Bump calculation for coordinated releases:** The root component bump type is determined by the highest impact change across all coordinated sub-components (e.g., if one patch + one minor → minor bump for root).

## Considered Options

I explicitly considered the following options before making the decision above. Recording them here improves traceability.

### Versioning options

* Per-component `VERSION` file: each component contains a `VERSION` file in its directory that is the working state for that component.
* Central `VERSIONS.yaml`: a single root `VERSIONS.yaml` that is the canonical snapshot of component versions (human-edited or bot-maintained).
* Hybrid: per-component `VERSION` files for working state, plus a generated `/VERSIONS.yaml` for published snapshots.

### Branching options

* Option A — Persistent component-scoped release branches: `releases/<component>/v<major>.<minor>` maintained as long-lived maintenance lines.
* Option B — Tag-only / short-lived release PRs: create ephemeral release-candidate branches or push tags directly; do not maintain long-lived release branches.

### Compatibility matrix (high-level)

| Versioning \\\ Branching | Option A (persistent branches) | Option B (tag-only / short-lived) |
| ----------------------: | :----------------------------: | :-------------------------------: |
| Per-component `VERSION`  | Good — native fit: branches host `VERSION`, easy hotfixes | OK — works, but hotfixes require creating temporary branches; more manual steps |
| Central `VERSIONS.yaml`  | OK — CI must reconcile branch-local changes into central file; potential conflicts | Fair — central file updated from tags only; high merge-conflict risk if many concurrent releases |
| Hybrid (chosen)         | Best — combines autonomy and a single snapshot; CI can generate `/VERSIONS.yaml` on publish | Good — possible but increases CI complexity to update central snapshot safely |

### Rejection rationale (short)

* Central-only `VERSIONS.yaml` + tag-only branching: rejected because hotfix/backport workflows become cumbersome and the central file becomes a high-conflict surface.
* Per-component `VERSION` without central snapshot: rejected because consumers (website, docs) need a single reproducible snapshot; lacking that reduces discoverability and reproducibility of root releases.

## Operational Rules and Rationale

### Authoritative sources

* Working state (pre-publication): `VERSION` file of the sub component located in, e.g., `cli/VERSION`or `kubernetes/VERSION` on the appropriate branch (either `main` for ongoing development, or a `releases/...` branch for maintenance lines).
* Published state: annotated Git tag `<component>/v<major>.<minor>.<patch>` — tags are authoritative for published artifacts and their provenance.
* Global snapshot: `/VERSIONS.yaml` — generated by CI after successful publishes and updated via OCM bot PRs; **this file is not intended for manual edits**.

Rationale: Having both per-component `VERSION` files (working state) and a generated `/VERSIONS.yaml` provides autonomy and a single snapshot for consumers.

### Branching model

* Create and maintain persistent branches for each active minor per component using the naming pattern: `releases/<component>/v<major>.<minor>`.
* `main` remains the integration branch for new features and non-release work.
* Release preparation: prepare a release branch from `main` when a new minor version should be released.
* Hotfixes: open PRs targeting the applicable `releases/<component>/vX.Y` branch; merge triggers a patch release from that branch.

### Special Case: OCM Root Component (No Release Branches)

The `ocm` root component does **not** use release branches - only tags created from `main`. Rationale:

* **OCM is a snapshot-only component** - it contains no independent code, only references to sub-component versions in `/VERSIONS.yaml`
* **No OCM-specific hotfixes exist** - all fixes happen at the sub-component level; OCM automatically reflects these changes  
* **Simplified workflow** - OCM releases are triggered by sub-component releases, not by independent development
* **Maintenance overhead reduction** - no additional branches to manage for a component that has no independent lifecycle

OCM release process: `main` + `/VERSIONS.yaml` update → `ocm/vX.Y.Z` tag (no intermediate branch required).

Rationale: Persistent release branches simplify backports and maintenance for components with independent code; they reduce the operational friction when maintaining older minor versions. For snapshot-only components like OCM, tags from main are sufficient.

### CI and automation responsibilities

#### Workflow: `create-release-branch`

**Trigger:** Manual workflow dispatch OR automated trigger when preparing new minor release

**Purpose:** Initialize a new persistent release branch for a component's minor version line

**Required Steps:**

* **Branch validation:** Verify that `releases/<component>/v<major>.<minor>` doesn't already exist
* **Source determination:** Create branch from `main` for new minor version release
* **Branch creation:** Create `releases/<component>/v<major>.<minor>` branch using OCM bot
* **VERSION file initialization:** Ensure component's `VERSION` file reflects the target minor version
* **Protection rules:** Apply branch protection (require PR reviews, status checks)
* **Notification:** Alert component maintainers of new release branch creation

**Manual Parameters:**

* `component`: Target component name (e.g., `ocm-cli`, `ocm-controller`)
* `major.minor`: Target version (e.g., `0.30` for `releases/ocm-cli/v0.30`)
* `source-ref`: Source branch/tag to create from (default: `main`)
* `initial-patch`: Starting patch version (default: `0`)

**Permissions Required:** OCM Bot token with branch creation and protection rule management

**Validation Checks:**

* Component exists in repository structure
* Source reference is valid and accessible
* Version format follows SemVer conventions
* No conflicting release branch already exists

**Error Handling:**

* Duplicate branch: Provide guidance on using existing branch or incrementing minor version
* Invalid component: List available components and naming conventions
* Permission failures: Alert repository administrators

**Note:** OCM root component does not require release branches - it uses tags directly from `main` branch.

#### Workflow: `release-from-branch`

**Trigger:** `push` to `releases/<component>/**` OR manual workflow dispatch

**Required Steps:**

* Pre-flight checks: validate branch naming convention and component existence
* Path filtering: only trigger if changes detected in component directory or VERSION file
* Run component-specific tests and verification
* Read `cli/VERSION` or `kubernetes/controller/VERSION` (fall back to latest component tag if absent)
* Compute new patch number using release tooling
* Create annotated tag `<component>/vX.Y.Z` using OCM bot
* Build and publish artifacts (binaries, container images, component descriptors)
* **Root component handling (configurable):**
  * **Default:** Calculate root component SemVer bump and trigger automatic `ocm` release
  * **Override:** Skip root release if `--suppress-root-release` flag is set (for coordinated releases)
* Update `/VERSIONS.yaml` via OCM bot PR with new sub-component version and computed root version (if applicable)
* Failure handling: if VERSIONS.yaml update conflicts, stop pipeline and alert maintainers

**Manual Parameters:**

* `suppress-root-release`: Boolean flag to prevent automatic OCM root release (for coordinated multi-component releases as described [above](#edge-case-coordinated-multi-component-releases))
* `release_candidate`: Boolean flag to create a release candidate tag (e.g., `v0.30.0-rc.1`) instead of a final release
* `release_candidate_name`: Release candidate suffix (e.g., `rc.1`, `rc.2`) when `release_candidate` is true

**Permissions Required:** Bot token with tag creation, PR creation, and artifact publishing rights

**Release Candidate Handling:**

* When `release_candidate = true`: Creates tag with `-rc.X` suffix (e.g., `ocm-cli/v0.30.0-rc.1`)
* Release candidate tags are NOT merged back into the release branch (preserves release notes generation)
* Only final releases (`release_candidate = false`) trigger merge-back and subsequent version bumps
* Release candidates allow testing and validation before final release

#### Workflow: `publish-from-tag`

**Trigger:** `push` tags matching `<component>/v*.*.*`

**Purpose:** Safety net and recovery mechanism for tag-based publishing scenarios

**Use Cases:**

* **Recovery scenario:** When `release-from-branch` fails after tag creation but before artifact publishing
* **Manual tag creation:** When tags are created manually outside the normal workflow (emergency fixes, migration scenarios)
* **Idempotent republishing:** When artifacts need to be republished for the same version (registry issues, corrupted uploads)

**Required Steps:**

* Verify tag provenance and format compliance
* Cross-check tag corresponds to actual component changes  
* Idempotency check: skip if artifacts already published for this tag
* Publish artifacts to GitHub Releases and container registries
* Update `/VERSIONS.yaml` if not already updated (e.g., for manual tags)

**Workflow Relationship:** This workflow complements `release-from-branch` but does NOT create tags - it only publishes artifacts for existing tags. The primary release path should always be `release-from-branch`.

**Error Handling:** Automatic retry on transient failures, alert on persistent issues

#### Workflow: `create-root-release` (Manual)

**Trigger:** Manual workflow dispatch only

**Use Case:** Consolidate multiple coordinated sub-component releases into a single OCM root release described [above](#edge-case-coordinated-multi-component-releases)

**Required Steps:**

* Read current `/VERSIONS.yaml` state to identify unreleased changes
* Validate that coordinated sub-component releases are completed
* Calculate appropriate root component SemVer bump (highest impact change across coordinated releases)
* Create/update `/VERSIONS.yaml` on `main` branch with consolidated changes
* Create annotated tag `ocm/vX.Y.Z` directly from `main` (no intermediate release branch)
* Generate consolidated release notes from all included sub-component changes
* Publish root component artifacts and update documentation

**Manual Parameters:**

* `included-components`: List of sub-components to include in this root release
* `bump-type`: Override for patch/minor/major (with justification)
* `release-notes`: Additional context for the coordinated release

**Permissions Required:** Same as standard release workflow plus read access to all sub-component release history

**Note:** OCM releases are created directly from `main` branch since OCM is a snapshot-only component with no independent development lifecycle.

Rationale: CI must be deterministic and make tags the definitive published-state indicator. `/VERSIONS.yaml` is a generated artifact updated after a successful publish to keep the website and documentation in sync.

### Tagging, naming and formats

* Branch naming: `releases/<component>/v<major>.<minor>` (component-first preferred).
* Tag naming: `<component>/v<major>.<minor>.<patch>` (annotated tags). Example: `ocm-cli/v0.30.1`.

### OCM Bot and security policy

* Use a dedicated bot account (our OCM Bot) with a scoped token for tag creation and `/VERSIONS.yaml` PRs.
* Bot actions must be auditable; bot PRs that update `/VERSIONS.yaml` must be reviewable and not merged automatically without required approvals if conflicts are possible.

### Hotfix/backport procedure

* Workflow: implement the fix on the persistent `releases/<component>/vX.Y` branch, merge, and let CI publish a patch tag. Then backport the change to `main` (or cherry-pick as appropriate).

## Operational Readiness

### Process Matrix

| Scenario | Trigger | Required Approvals | Rollback Strategy |
|----------|---------|-------------------|-------------------|
| Regular Release | PR merge to `releases/<component>/**` | Component maintainers | Tag deletion + revert commit |
| Hotfix Release | PR merge to `releases/<component>/**` | Component maintainers | Emergency revert + new patch |
| Root Release (Auto) | PR merge to `main` | Component maintainers | Revert VERSIONS.yaml + republish |
| Coordinated Release | Manual workflow dispatch | All maintainers | Revert all coordinated changes |

**Coordinated Release Process:**

1. Create required sub-component release branches (e.g., `releases/ocm-cli/v0.30`, `releases/ocm-controller/v0.29`)
2. Release sub-components with `--suppress-root-release` flag  
3. Validate integration of all coordinated changes
4. Manual trigger of `create-root-release` workflow (operates directly on `main` - no OCM release branch needed)
5. Single OCM root release tag created from `main` containing all coordinated changes

### Failure Scenarios and Recovery

| Failure Type | Detection | Recovery Action | Prevention |
|--------------|-----------|-----------------|------------|
| Bot token expired | CI pipeline failure | Manual token rotation + retry | Automated token monitoring |
| VERSIONS.yaml conflict | Merge conflict in OCM bot PR | Human resolution + manual retry | Serialized release coordination |
| Artifact upload failed | Publish step failure | Automatic retry + maintainer alert | Pre-flight connectivity checks |
| Test failures | Pre-publish validation | Block release + notify maintainer | Mandatory local testing |
| Tag collision | Tag creation failure | Error + guidance to use next patch number | Atomic tag creation checks |

## Final Decision

This ADR documents the chosen strategy (Hybrid versioning + component-scoped persistent release branches) and the rationale for rejecting alternatives. It supersedes earlier separate documents on versioning and branching and is the canonical operational specification for releases.
