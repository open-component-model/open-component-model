# Versioning Strategy for Components in a Monorepo

* Status: Proposed
* Deciders: Gergely Brautigam, Fabian Burth, Jakob Moeller, Gerald Morrison, Ilya Khandamirov, Piotr Janik, Matthias Bruns
* Date: 2025-09-12

## Context

We maintain a monorepo with multiple components: `ocm-cli`, `ocm-controller`, and a possible root/wrapper component `ocm` (final name to be decided. Solely acts as a root component referencing versions of the sub-components).

Each sub-component evolves at its own pace, and the root component should always represent a snapshot of sub-component versions (and all their Go modules in the OCM Library).

We need a versioning strategy that:

* Ensures immutable, reproducible releases.
* Supports independent development of sub-components.
* Provides a clear snapshot for documentation and website purposes.
* Can be automated via CI/CD (GitHub Actions).

## Decision Drivers

* Autonomy of sub-component versioning
* Central overview for root component and documentation
* Automation potential and minimal manual intervention
* SemVer compliance for both sub-components and root component
* Ease of use for website and other consumers

## Out of scope

* Decision on release frequency and synchronization of releases for sub-components.

## Considered Options

### Option A: One `VERSION` File per Component

**Structure:**

```text
/VERSION          <- Root version (ocm)
ocm-cli/VERSION
kubernetes/controller/VERSION
```

**Advantages:**

* Clear separation per component.
* Independent version management.
* Simple automation per component.

**Disadvantages:**

* Root version must be maintained separately.
* No central overview.
* Website needs to digest and merge multiple files.

---

### Option B: Central `VERSIONS.yaml` in Root

**Structure:**

```yaml
ocm: v0.31.0
ocm-cli: v0.30.2
ocm-controller: v0.29.0
```

**Advantages:**

* All versions in a single snapshot (aka component for `ocm`).
* Directly usable by website.
* Programmatically accessible for CI/CD.

**Disadvantages:**

* Sub-components must push versions to the central file.
* Potential merge conflicts.
* Reduced autonomy for sub-components.

---

### Option C: Hybrid – Sub-Component `VERSION` Files + Root `VERSIONS.yaml`

**Structure:**

```text
/VERSION          <- ocm (root)
/VERSIONS.yaml    <- snapshot of all components
ocm-cli/VERSION
kubernetes/controller/VERSION
```

As a simplification, the root VERSION file for ocm can be omitted entirely, with the VERSIONS.yaml serving as the single source of truth. It contains both the root and all sub-component versions, simplifying automation and ensuring a consistent snapshot for releases and documentation.

**Advantages:**

* Sub-components retain autonomy.
* Central snapshot for root component.
* CI/CD can automatically generate `VERSIONS.yaml`.
* Clear and reproducible for website and documentation.
* SemVer compliance for all releases.

**Disadvantages:**

* Two version locations (component + snapshot).
* Slightly more complex automation.

---

## Principles for SemVer of `ocm` Root Component

* **Patch bump (x.y.z → z++)**: triggered by any sub-component patch release.
* **Minor bump (x.y.z → y++)**: triggered by any sub-component feature addition.
* **Major bump (x.y.z → x++)**: triggered by any sub-component breaking change.

Every OCM release is a reproducible snapshot and maintains SemVer compliance.

---

## Comparison Table

| Option | Automatable | Visibility | Sub-Component Autonomy | Merge Conflict Risk | Notes |
| ------ | ----------- | --------- | ---------------------- | ------------------- | ----- |
| A – separate `VERSION` files | high | medium | high | low | Simple per-component automation; no central snapshot |
| B – central `VERSIONS.yaml` | high | high | low/medium | medium | Single snapshot for website/CI; requires push/update discipline |
| C – hybrid | high | high | high | medium | Combines autonomy and central snapshot; slightly more automation complexity |

## Decision Outcome

Adopt Option...
