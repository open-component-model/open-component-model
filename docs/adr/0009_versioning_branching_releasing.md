# ADR 0009 — Versioning, Branching and Releasing for the OCM Monorepo

* Status: Proposed
* Deciders: OCM Technical Steering Committee
* Date: 2025-09-16

Technical Story: Define a single, operational specification for component versioning, release branching, and CI publishing for the OCM monorepo using Git tags as the single source of truth.

## Context and Problem Statement

The repository contains multiple independently developed components, currently our `cli` and `controller`. There will be an additional root `ocm` component that combines specific versions of all sub-components into a tested `ocm` release.

The short forms of the components used in this ADR are related to the following OCM component IDs:

* `ocm` -> `ocm.software/ocm`
* `cli` ->  `ocm.software/cli`
* `controller` -> `ocm.software/kubernetes/controller`

We require a single, operational specification that defines how components are versioned, how release branches are created and maintained, and how CI publishes releases.

## Decision Drivers

* Reproducible, auditable published releases.
* Allow a release per component without forcing simultaneous releases of all components.
* Support simple and robust hotfix and backport workflows for older minor releases.
* Provide a machine-readable snapshot for tested combinations of sub-components in form of an OCM Component Constructor YAML file.
* Keep CI complexity manageable and deterministic.
* Ensure immutable releases for secure supply chain compliance and tamper-proof artifacts.

## Proposal

We propose using Git tags instead of version files to track software versions. This approach uses component-specific version tags and dedicated release branches for updates, while leveraging an OCM Component Constructor YAML file to manage how all the parts work together.

Key elements:

* Git tags replace VERSION files as the authoritative source for component versions
* Component-scoped release branches for maintenance (`releases/<component>/v<major>.<minor>`)
* OCM Component Constructor YAML manages version matrix and releases from `main` branch
* Conformance tests validate sub-component combinations
* Immutable GitHub releases with protected tags and signed attestations ensure supply chain security

All components will run conformance tests to ensure compatibility. Tests will be created over time and are not mandatory for initial rollout.

## High-level Architecture

All component versions are determined exclusively from Git tags:

```text
Repository Structure:
├── cli/                     # OCM CLI component
├── kubernetes/controller/   # OCM Controller component  
├── ocm/                     # `ocm` root component
│   ├── component-constructor.yaml  # Version matrix
│   └── conformance/              # Integration conformance tests
└── .github/workflows/      # CI/CD automation

Tag-based Versioning:
- cli/v0.30.1                    # CLI component release
- kubernetes/controller/v0.29.2  # Controller component release
- ocm/v0.11.0                    # OCM root release (aggregates sub-components)
```

### Release Branch Strategy

Sub-components use persistent release branches for maintenance:

* `releases/cli/v0.30` - CLI v0.30.x maintenance line
* `releases/kubernetes/controller/v0.29` - Controller v0.29.x maintenance line

`ocm` root component releases directly from `main` branch with no release branches.

## Component Relationship Model

### Component Independence

* **No Direct Dependencies**: CLI and Controller are developed and released independently with no direct dependencies between them, though both share the common OCM library as their foundation
* **Bundle Validation**: `ocm` root component validates that specific combinations of sub-components work together through integration testing
* **Breaking Changes Policy**: Following Kubernetes model, breaking changes are permitted in minor releases across all components

### OCM Root as Integration Bundle

* **Purpose**: `ocm` root component serves as a tested bundle of working sub-component releases, containing no independent business logic
* **Validation**: Conformance tests ensure the specific combination of sub-component versions operates correctly together
* **Release Trigger**: Any final sub-component release automatically triggers OCM release candidate evaluation to create new tested bundles

## Contract

### Sub-Component Release Process

Sub-components (currently CLI and Controller) follow a structured release process using persistent release branches:

#### Release Branch Creation

**When**: Before releasing a new minor version of a sub-component

**Process**:

1. Create release branch from `main`: `releases/<component>/v<major>.<minor>`
2. Apply branch protection rules
3. Initial patch version (`.0`) is released from this branch

**Example**: For CLI v0.31.0, create `releases/cli/v0.31`

#### Release Workflow for Sub-Components

Sub-components follow a two-phase release process:

##### Phase 1: Release Candidate Creation

* Create and test release candidates from release branches
* Generate RC tags for validation: `<component>/v<major>.<minor>.<patch>-rc.N`
* Extended testing and validation period before final release

##### Phase 2: Final Release Promotion

* Promote validated RC to final release
* Create final Git tags: `<component>/v<major>.<minor>.<patch>`
* Automatically trigger OCM root component release candidate evaluation
* Publish final artifacts and release notes

*Detailed technical workflow steps are described in the [CI/CD Workflows section](#workflow-sub-component-release).*

#### Hotfix/Backport Procedure

**Process**:

1. Implement fix on the `main` branch
2. Cherry-pick fix to relevant local release branch
3. Open PR targeting the remote release branch

**Example**: Hotfix for CLI v0.30.2 goes to `releases/cli/v0.30` → `cli/v0.30.2`

#### Sub-Component Release Types

All sub-component releases follow a streamlined process with RC workflow only required for initial minor version releases:

* **Minor Version Releases**: Create new release branch → RC workflow → final release (e.g., cli/v0.30.0)
* **Patch Releases**: Use existing release branch → direct final release (e.g., cli/v0.30.1, cli/v0.30.2)
* **RC Requirement**: Only the initial minor version (.0) requires RC workflow; subsequent patches are released directly from the release branch
* **Impact on `ocm` root component**: final release tag triggers new OCM RC creation with incremented number

### OCM Root Component Strategy

The `ocm` root component utilizes an **OCM Component Constructor YAML** located in `/ocm/component-constructor.yaml` as the authoritative version matrix. This constructor:

* References specific versions of all sub-components (cli, controller, etc.)
* Acts as the definitive snapshot for reproducible OCM releases
* Is automatically updated via bot PRs when sub-component releases trigger OCM releases

#### Template for `/ocm/component-constructor.yaml`

```yaml
components:
  - name: ocm.software/ocm
    version: ${OCM_VERSION}  # will be resolved at release time
    provider: ocm.software
    
    componentReferences:
      - name: cli
        componentName: ocm.software/cli
        version: ${OCM_CLI_VERSION}  # will be resolved at release time

      - name: controller
        componentName: ocm.software/kubernetes/controller
        version: ${OCM_CONTROLLER_VERSION}  # will be resolved at release time
```

### OCM Root Component Release Process

**Key Principle**: Sub-component releases trigger OCM version bumps:

* Sub-component patch/minor → OCM minor bump
* Sub-component major → OCM major bump

**Release Workflow**:

1. Sub-component releases (e.g., `cli/v0.30.1`)
2. **RC Creation Logic**:
   * Check for existing OCM RCs for target version
   * If no final release exists: Create new RC with incremented number
   * If final release exists: Create RC for next version
3. Update component constructor with new sub-component versions
4. Run integration conformance tests (if available)
5. Manual promotion: RC → Final release

### Failure Recovery for OCM Root Component

**When OCM RC conformance tests fail**:

1. **Analysis**: Identify which sub-component interaction caused the failure
2. **Patch Release**: Create patch releases for affected sub-components
3. **New RC**: Sub-component patch automatically creates new OCM RC with incremented number
4. **Re-test**: Run conformance tests for new RC with updated components
5. **Iterate**: Repeat until conformance tests pass

**Example**: `ocm/v0.12.0-rc.1` fails → `cli/v0.31.1` patch → `ocm/v0.12.0-rc.2` with CLI v0.31.1

### OCM Integration Testing

The `/ocm/` directory contains:

* **Component Constructor**: `/ocm/component-constructor.yaml` - defines sub-component versions and relationships
* **Conformance Tests**: `/ocm/tests/` - integration tests covering scenarios where multiple components interact. These tests validate that the specific combination of sub-component versions works together as expected. They will be created over time and are not mandatory for the initial rollout of this strategy.
* **Test Scenarios**: End-to-end workflows validating component interoperability beyond individual component tests

These conformance tests are executed during the RC-to-final promotion process to ensure component compatibility.

## CI/CD Workflows and Automation

The release strategy is implemented through several automated workflows that handle different aspects of the release process:

### Workflow: `create-release-branch`

**Trigger**: Manual workflow dispatch OR automated trigger when preparing new minor release

**Purpose**: Initialize a new persistent release branch for a component's minor version line

**Required Steps**:

1. **Branch validation**: Verify that `releases/<component>/v<major>.<minor>` doesn't already exist
2. **Source determination**: Create branch from `main` for new minor version release
3. **Branch creation**: Create `releases/<component>/v<major>.<minor>` branch using OCM bot
4. **Protection rules**: Apply branch protection rules identical to `main` branch (require PR reviews, status checks, admin override settings)

**Note**: `ocm` root component does not require release branches - it uses tags directly from `main` branch.

### Workflow: `sub-component-release`

**Trigger**: Manual workflow dispatch

**Purpose**: Handle release candidate creation and promotion to final release for sub-components

**Inputs**:

* `component`: Component to release (e.g., "cli", "controller")
* `release_candidate`: Boolean - true for RC creation, false for final release promotion

**For Release Candidate Creation** (`release_candidate: true`):

1. Validate branch and component existence
2. Calculate next patch version and RC number
3. Run component tests and build RC artifacts
4. Create RC tag and trigger validation workflows
5. Notify maintainers

**For Final Release Promotion** (`release_candidate: false`):

1. Verify RC exists and validation completed
2. Create final tag and publish artifacts
3. Trigger OCM root component integration (version calculation, RC creation, testing)
4. Create immutable GitHub release with attestations
5. Notify maintainers or handle failures

### Workflow: `ocm-component-release`

**Trigger**: Automated trigger from all sub-component final releases AND manual workflow dispatch

**Purpose**: Handle OCM release candidate creation and promotion to final release

**Inputs**: `release_candidate`: Boolean - true for RC creation, false for final release promotion

**For Release Candidate Creation** (`release_candidate: true`):

1. Calculate next OCM version based on sub-component changes
2. Update `/ocm/component-constructor.yaml` with latest sub-component versions
3. Create RC tag directly from `main`
4. Run conformance tests (if available)
5. Notify maintainers

**For Final Release Promotion** (`release_candidate: false`):

1. Validate latest RC exists and tests passed
2. Create final tag from `main`
3. Generate release notes linking to sub-component releases
4. Publish artifacts and create immutable GitHub release
5. Notify maintainers

### Tagging, Naming and Formats

* **Branch naming**: `releases/<component>/v<major>.<minor>` (for sub-components only)
* **Tag naming**: `<component>/v<major>.<minor>.<patch>` (annotated tags)
* **Examples**:
  * `cli/v0.30.1` (CLI patch release)
  * `kubernetes/controller/v0.29.2` (Controller patch release)
  * `ocm/v0.11.0` (OCM root release)

## Operational Rules and Implementation Details

### Authoritative Sources

* **Published state**: Annotated Git tag `<component>/v<major>.<minor>.<patch>` — tags are authoritative for published artifacts and their provenance
* **OCM version matrix**: `/ocm/component-constructor.yaml` — OCM Component Constructor defining sub-component versions and relationships for OCM releases

### Immutable Releases and Supply Chain Security

All final releases are published as [immutable GitHub releases](https://github.blog/changelog/2025-08-26-releases-now-support-immutability-in-public-preview/) to ensure supply chain security:

* **Immutable Assets**: Once published, release assets cannot be added, modified, or deleted
* **Protected Tags**: Tags for immutable releases are automatically protected from deletion or movement
* **Signed Attestations**: All releases receive signed attestations using Sigstore bundle format for verification
* **Verification**: Release integrity can be verified using GitHub CLI: `gh release verify <tag>` and `gh release verify-asset <tag> <asset>`
* **Repository Configuration**: Immutability is enabled at the repository level and applies to all new releases

### Release Branch Lifecycle

* **Creation**: Release branches created from `main` when starting new minor version development
* **Maintenance**: Persistent branches maintained for active minor versions requiring patches
* **Protection**: Branch protection rules prevent direct pushes and require PR reviews
* **Archival**: Old release branches archived when no longer maintained (policy TBD)

### Integration with `ocm` Root Component Releases

Every sub-component release automatically triggers `ocm` root component release candidate evaluation:

1. **OCM version calculation**: Apply minor version bump to `ocm` root component (patch/minor sub-component changes → OCM minor bump, major sub-component changes → OCM major bump)
2. **Component constructor update**: Automatic PR against `main` branch to update `/ocm/component-constructor.yaml` with new sub-component versions
3. **RC Creation**: After PR merge, automatically create new OCM release candidate with incremented RC number
4. **Integration testing**: Conformance tests validate component combinations for the new RC
5. **Manual promotion gate**: Human approval required for final OCM releases (RC → Final always manual)

### Release Notes Strategy

OCM releases use simplified aggregated release notes that reference existing sub-component releases rather than duplicating content:

```text
## 📦 Included Components

- **OCM CLI**: [`v0.30.1`](link-to-cli-release)
- **OCM Controller**: [`v0.29.3`](link-to-controller-release)

---
*This OCM release bundles the above tested and compatible component versions.*
```

This approach leverages existing sub-component release notes while maintaining traceability and avoiding content duplication.

## Release Process Examples

### Example 1: Single Sub-Component Release

```text
Day 1: ocm/v0.10.0 (final) with cli/v0.30.0 + kubernetes/controller/v0.29.2

Day 2: CLI Release
- 09:00 - Create cli/v0.31.0-rc.1
- 10:00 - Promote to cli/v0.31.0 (final)
- 10:01 - Auto-create ocm/v0.11.0-rc.1 with new CLI version
- 10:05 - Run conformance tests

Day 3: OCM Release
- 09:00 - Promote ocm/v0.11.0-rc.1 → ocm/v0.11.0 (final)
```

### Example 2: Coordinated Multi-Component Release

```text
Day 1: ocm/v0.11.0 (final) with cli/v0.30.1 + kubernetes/controller/v0.29.2

Day 2: First Sub-Component Release
- 14:00 - cli/v0.31.0 released (final)
- 14:01 - Auto-create ocm/v0.12.0-rc.1 with new CLI version

Day 3: Second Sub-Component Release
- 10:00 - kubernetes/controller/v0.30.0 released (final)
- 10:01 - Auto-create ocm/v0.12.0-rc.2 with both new components
- 10:03 - Run conformance tests for new RC

Day 4: OCM Final Release
- 11:00 - Promote ocm/v0.12.0-rc.2 → ocm/v0.12.0 (final)
- Note: ocm/v0.12.0-rc.1 remains in Git history with only CLI v0.31.0
```

### Example 3: RC Failure and Recovery

```text
Day 1: ocm/v0.11.0 (final) with cli/v0.30.1 + kubernetes/controller/v0.29.2

Day 2: CLI Release triggers OCM RC
- 10:00 - cli/v0.31.0 released (final)
- 10:01 - Auto-create ocm/v0.12.0-rc.1 with new CLI
- 10:30 - OCM conformance tests FAIL

Day 3: Recovery
- 10:00 - cli/v0.31.1 hotfix released
- 10:01 - Auto-create ocm/v0.12.0-rc.2 with CLI fix
- 10:30 - OCM conformance tests PASS

Day 4: Final OCM Release
- 11:00 - Promote ocm/v0.12.0-rc.2 → ocm/v0.12.0 (final)
```

## Pros and Cons of the Proposal

Pros:

* **Simplicity**: Git tags are Git-native and require no additional file maintenance
* **Auditability**: Complete version history is preserved in Git repository
* **Go Module Compatibility**: Tag naming follows Go module conventions exactly
* **Reduced Merge Conflicts**: No version files to conflict during parallel releases
* **Single Source of Truth**: Git tags eliminate version drift between files and actual releases
* **OCM Native**: Component Constructor used as version matrix follows OCM specification patterns
* **Supply Chain Security**: Immutable releases with protected tags, locked assets and signed attestations prevent tampering and ensure artifact integrity

Cons:

* **Tag Dependency**: CI/CD pipelines must be robust in Git tag parsing and validation
* **Learning Curve**: Teams need to understand Git tagging workflows for version management

## Conclusion

This Git tag-based versioning strategy with OCM Component Constructor provides a robust, Git-native approach to monorepo component versioning and releasing. By eliminating version files and using Git tags as the single source of truth, we reduce maintenance overhead while ensuring full compatibility with Go module conventions and OCM specifications.

The `ocm` root component serves as a tested integration point for sub-components, providing users with verified compatible component combinations while maintaining the flexibility for independent sub-component releases.
