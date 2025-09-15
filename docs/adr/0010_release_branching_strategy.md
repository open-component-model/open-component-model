# Release Branching Strategy for the OCM Monorepo

* Status: Proposed
* Deciders: Gergely Brautigam, Fabian Burth, Jakob Moeller, Gerald Morrison, Ilya Khandamirov, Piotr Janik, Matthias Bruns
* Date: 2025-09-15

## Context

The monorepo contains multiple independently developed components, e.g. `ocm-cli`, `ocm-controller`, and optionally a root/wrapper component `ocm` (final name to be decided. Acts as a root component referencing versions of the sub-components).

Each component should be able to evolve and be versioned independently while we maintain reproducible releases, simple automation (GitHub Actions) and clear documentation of published versions.

This ADR addresses release-branch strategy: how release branches are named, created, maintained and how hotfixes are handled when components are released independently.

This ADR complements the [component versioning ADR](https://github.com/open-component-model/open-component-model/pull/881) and refers to the [EPIC: Implement Automated Release Process for OCM Monorepo](https://github.com/open-component-model/ocm-project/issues/645) and the Issue: [#647](https://github.com/open-component-model/ocm-project/issues/647).

## Decision drivers

* Component independence in the release process
* Minimal complexity for maintainers
* Support for automated workflows (build, publish, OCM components for all components)
* Traceability / auditability of releases
* SemVer-compliant versioning
* Handling hotfixes and cherry-picks

## Goals

* Each component can publish releases independently.
* Release branches are clearly named and easy to maintain.
* CI/CD workflows can be scoped to branches.
* Hotfixes are possible without unnecessary synchronization across components.

## Scenarios / Options

We keep exactly two options for clarity. Option A is a component-scoped, persistent-branch model (with two naming variants). Option B is a tag-only / short-lived release-PR model.

### Option A — Component-scoped release branches (recommended default)

Description:

For each component, maintain dedicated release branches in the monorepo. Two naming variants are allowed:

* Variant A1 (component-first namespace, **preferred**): `releases/<component>/v<major>.<minor>`
  * Examples: `releases/ocm-cli/v0.30`, `releases/ocm-controller/v0.29`
* Variant A2 (major-first namespace, organizational alternative): `releases/v<major>/<component>/v<major>.<minor>`
  * Example: `releases/v0/ocm-cli/v0.30` (useful when the organisation emphasises major lines)

Workflow / triggers:

* CI workflows listen on the appropriate prefix (`releases/<component>/**` or `releases/v<major>/**`) and build/publish only artifacts for the affected component.
* Tagging: when a release is published from the branch, create an annotated tag like `ocm-cli/v0.30.1` (or `ocm-cli@v0.30.1`, final naming to be decided)).

Hotfixes / cherry-picks:

* Hotfixes are committed to the respective release branch (per-minor maintenance branch) and deployed from there.
* If a hotfix must also land in other branches, apply it via cherry-pick to `main` or other release lines as appropriate.

Advantages:

* Clear separation and low cognitive load for component maintainers.
* CI scales well: workflows are component-scoped.
* Supports multiple parallel minor/major lines per component.

Disadvantages:

* More branches to maintain (but organised).
* Coordination required when multiple components need synchronized releases.

Notes on the Variant choice (A1 / A2) :

* Variant A1 (component-first) is easier for teams focusing on per-component maintenance and for workflows that rely on simple branch-prefix filters.
* Variant A2 (major-first) is useful if the organisation manages support per major (e.g. central teams owning `v1`, `v2`).

---

### Option B — Tag-only releases + short-lived release PRs

Description:

Do not keep long-lived release branches. Instead create short-lived release candidate PR branches (e.g. `release-candidate/<component>/vX.Y`) from `main`, publish from those PR branches and delete them after the release. Releases are persisted via tags and published artifacts.

Workflow / triggers:

* Release PRs trigger build and publish pipelines. After publishing, an annotated tag is created and the RC branch is deleted.

Hotfixes / cherry-picks:

* Hotfixes are created as new release PRs or as direct tag+patches on `main`. The release history is preserved through tags and GitHub Releases.

Advantages:

* Low branch proliferation.
* Easier cleanup; history preserved in tags/releases.

Disadvantages:

* No stable support branches for long-term maintenance (e.g. v1.x). Hotfixes for older releases become more complex without a persistent branch.

## Evaluation / comparison

| Option | Automatable | Visibility | Sub-Component Autonomy | Merge Conflict Risk | Maintenance / Backporting | Notes |
| ------ | ----------- | --------- | ---------------------- | ------------------- | ------------------------ | ----- |
| Option A — component-scoped branches (A1/A2/A3 variants) | high | high | high | medium | high | Preferred: clear per-component maintenance lines; persistent minor branches simplify backports |
| Option B — tag-only / short-lived PRs | high | medium | high | low | low | Low branch proliferation; harder for long-term support/backports |

Explanation: Option A is the recommended approach because it offers the clearest operational model for per-component maintenance while remaining automatable. Option B is suitable when branch proliferation must be minimised and the team does not plan long-term maintenance on older minor lines.

## Recommendation

Adopt Option A: component-scoped release branches under `releases/<component>/vX.Y` (with the A1 component-first layout preferred). Allow the A2 major-first variant or A3 flat-name alternative only for specific tool compatibility or organizational reasons.

Rationale:

* Maximum clarity and reduced risk during build/publish operations.
* CI workflows can be trivially scoped per component using branch prefixes.
* Supports parallel minor/major lines per component and straightforward hotfix maintenance.

Recommended conventions:

* Branch name pattern (preferred): `releases/<component>/v<major>.<minor>`
* Release tags: `<component>/v<major>.<minor>.<patch>` or `<component>@v<major>.<minor>.<patch>` (preference: slash-separated e.g. `ocm-cli/v0.30.1`)
* Workflows: place component-specific workflows in `/.github/workflows` with triggers:
  * `on: push` for branches: `releases/<component>/**`
  * path filters for the component sources and artifacts
* Hotfix process: commit patch to `releases/<component>/v<major>.<minor>` → CI builds + publishes patch → optionally cherry-pick into `main` and other lines if relevant.

## Additional notes

* On adoption: provide automated GitHub Actions to create initial `releases/<component>/v<current-minor>` branches from existing tags or `VERSION` files.
* Add release/hotfix PR templates and a `CONTRIBUTING.md` section describing steps.
* Recommend branch protection rules on `releases/<component>/**` (require PR reviews, signed commits if needed).

## Open points

* Finalize tag format (slash vs `@`).

## Decision

Adopt Option...
