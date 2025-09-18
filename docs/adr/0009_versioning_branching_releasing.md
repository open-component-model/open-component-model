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
* Provide a machine-readable snapshot for documentation and website consumption.
* Keep CI complexity manageable and deterministic.
* Ensure immutable releases for secure supply chain compliance and tamper-proof artifacts.

## Proposal

We propose a Git-native versioning strategy that eliminates version files in favor of authoritative Git tags as the single source of truth. This approach combines SemVer-compliant Git tag-based versioning for individual sub-components with component-scoped persistent release branches for maintenance workflows, while leveraging an OCM Component Constructor YAML for managing the root OCM component's version matrix and coordinated releases.

All components will have to run extensive conformance tests to ensure compatibility between components. The tests will be created over time and are not mandatory for the initial rollout of this strategy.

## Description

We adopt a Git-native, tag-based versioning strategy that eliminates version files in favor of authoritative Git tags:

* **Versioning**: Git tags are the single source of truth for component versions - no VERSION files are maintained in the repository
* **Branching**: Component-scoped persistent release branches following the pattern `releases/<component>/v<major>.<minor>` for maintenance lines
* **Publishing**: Annotated Git tags with Go module-compatible naming (`<component>/v<major>.<minor>.<patch>`) serve as the authoritative record of published artifacts
* **OCM Root Component**: Uses OCM Component Constructor YAML as version matrix and releases directly from `main` branch without release branches
* **Integration Validation**: Dedicated conformance tests at the `ocm` root component level validate sub-component interoperability and ensure tested combinations before final releases of the `ocm` root component.
* **Immutable Releases**: All GitHub releases are configured as immutable with protected tags and signed attestations to prevent tampering and ensure supply chain security

## High-level Architecture

All component versions are determined exclusively from Git tags:

```text
Repository Structure:
├── cli/                     # OCM CLI component
├── kubernetes/controller/   # OCM Controller component  
├── ocm/                     # `ocm` root component
│   ├── component-constructor.yaml  # Version matrix
│   └── tests/              # Integration conformance tests
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

* **No Hard Dependencies**: CLI and Controller components are developed and released independently with no direct runtime dependencies between them
* **Bundle Validation**: `ocm` root component validates that specific combinations of sub-components work together through integration testing
* **Breaking Changes Policy**: Following Kubernetes model, breaking changes are permitted in minor releases across all components
* **Compatibility Scope**: Version compatibility exists only within individual sub-components (e.g., cli/v0.30.x patch compatibility), not across different components

### OCM Root as Integration Bundle

* **Purpose**: `ocm` root component serves as a tested bundle of working sub-component releases, containing no independent business logic
* **Validation**: Conformance tests ensure the specific combination of sub-component versions operates correctly together
* **Version Matrix**: Component Constructor YAML defines the tested combination without imposing runtime dependencies
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

1. Implement fix on the appropriate `releases/<component>/vX.Y` branch
2. Open PR targeting the release branch
3. After merge, backport change to `main` branch (cherry-pick or separate PR) if applicable

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

**Key Principle**: Any sub-component release of type **patch** and **minor** triggers an OCM **minor** version bump, as each represents a new combination of tested components. A **major** release of a sub-component triggers an OCM **major** version bump.

**Versioning Logic**:

* Sub-component change: patch/minor → OCM: MINOR bump
* Rationale: Each OCM release represents a new tested combination of sub-components

**Release Workflow**:

1. Sub-component releases and creates tag (e.g., `cli/v0.30.1`)
2. **RC Detection Logic**:
   * Check for existing `ocm` root component RCs for the target version (e.g., `ocm/v0.11.0-rc.*`)
   * Check if corresponding final release exists (e.g., `ocm/v0.11.0`)
   * **If no final release exists**: Create new RC with incremented number (rc.1, rc.2, rc.3, ...)
   * **If final release exists**: Create new RC for next version (v0.12.0-rc.1)
3. Update component constructor with new sub-component versions
4. Run integration conformance tests from `/ocm/tests/` (if available)
5. Manual promotion: RC → Final release (e.g., `ocm/v0.11.0`)

### Failure Recovery for OCM Root Component

**When OCM Root Component RC Conformance Tests Fail**:

1. **Root Cause Analysis**: Identify which sub-component interaction(s) caused the failure
2. **Sub-component Patch Release**: Create patch releases for affected sub-components on their respective release branches
3. **Patch Validation**: Each sub-component patch goes through normal testing (hotfix releases bypass RC workflow)
4. **RC Strategy**: Each sub-component patch release creates a new RC with incremented number (following standard RC numbering)
5. **Re-test Integration**: Run conformance tests for the new RC with updated sub-component versions
6. **Iterative Process**: Repeat until conformance tests pass

**Example**:

```text
ocm/v0.12.0-rc.1 fails → cli/v0.31.1 patch → ocm/v0.12.0-rc.2 with CLI v0.31.1
```

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
5. **Notification**: Alert component maintainers of new release branch creation

**Note**: `ocm` root component does not require release branches - it uses tags directly from `main` branch.

### Workflow: `sub-component-release`

**Trigger**: Manual workflow dispatch

**Purpose**: Handle both release candidate creation and promotion to final release for sub-components

**Inputs**:

* `component`: Component to release (e.g., "cli", "controller")
* `release_candidate`: Boolean - true for RC creation, false for final release promotion

**Required Steps**:

**For Release Candidate Creation** (`release_candidate: true`):

1. **Pre-flight checks**: Validate branch naming convention and component existence
2. **Version calculation**: Determine next patch version from existing tags on branch
3. **RC number calculation**: Determine next RC number automatically (rc.1, rc.2, rc.3, ...)
4. **Component testing**: Run component-specific tests and verification
5. **Artifact creation**: Build and publish RC artifacts (binaries, container images, component descriptors)
6. **RC tag creation**: Create annotated tag `<component>/vX.Y.Z-rc.N` using OCM bot
7. **RC validation**: Trigger extended validation and testing workflows
8. **Notification**: Alert component maintainers of RC availability for validation

**For Final Release Promotion** (`release_candidate: false`):

1. **RC validation**: Verify that latest RC exists and validation completed
2. **Final tag creation**: Create annotated tag `<component>/vX.Y.Z` using OCM bot from RC commit
3. **Final artifact publishing**: Publish final artifacts to registries
4. **OCM Integration**:
   * **Determine target `ocm`root component version**: Calculate next `ocm`root component version based on sub-component change type
   * **RC Detection**: Check for existing RC tags for target version (e.g., `ocm/v0.12.0-rc.*`)
   * **Final Release Check**: Verify no final release exists for target version (e.g., `ocm/v0.12.0`)
   * **RC Reuse Logic**:
     * **If no final release exists**: Create new RC with incremented number for target version
     * **If final release exists**: Create new RC for next available version
   * **Integration Testing**: Run conformance tests for the new RC
5. **GitHub Release**: Create immutable GitHub release with component-specific release notes
6. **Notification**: Alert maintainers of successful sub-component release
7. **Failure handling**: If any step fails, stop pipeline and alert maintainers

### Workflow: `ocm-release`

**Trigger**: **Automated trigger from all sub-component final releases** (patch, minor, major) AND manual workflow dispatch

**Purpose**: Handle both OCM release candidate creation and promotion to final release

**Inputs**:

* `release_candidate`: Boolean - true for RC creation, false for final release promotion

**Required Steps**:

**For Release Candidate Creation** (`release_candidate: true`):

1. **Version calculation**: Determine next OCM minor version based on sub-component changes
2. **RC number calculation**: Determine next RC number for target version (rc.1, rc.2, rc.3, ...)
3. **Component constructor update**: Update `/ocm/component-constructor.yaml` with latest sub-component versions
4. **RC tag creation**: Create annotated tag `ocm/vX.Y.Z-rc.N` directly from `main`
5. **Integration testing**: Run conformance tests from `/ocm/tests/` (if available)
6. **Notification**: Alert maintainers of OCM RC availability for validation

**For Final Release Promotion** (`release_candidate: false`):

1. **RC validation**: Automatically find and validate the latest RC for the target version (verify it exists and conformance tests have passed)
2. **Version resolution**: Resolve final sub-component versions from the latest RC
3. **Component constructor finalization**: Finalize `/ocm/component-constructor.yaml` with RC's component versions
4. **Tag creation**: Create annotated tag `ocm/vX.Y.Z` directly from `main`
5. **Release notes generation**: Generate simplified release notes linking to sub-component releases
6. **Artifact publishing**: Publish OCM artifacts and update documentation
7. **Immutable GitHub Release**: Create immutable GitHub release with security protections
8. **Notification**: Alert maintainers of successful OCM release

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
   * **PR Creation**: OCM bot creates PR with updated component references
   * **PR Content**: Updated component versions, OCM version bump, automatic change summary
   * **PR Review Options**:
     * **Option A**: Auto-merge after successful CI checks (faster, suitable for non-breaking changes)
     * **Option B**: Manual review and approval required (safer, allows human oversight)
   * **Implementation Decision**: The choice between auto-merge vs manual review can be configured per deployment
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
Day 1: Current state
- ocm/v0.10.0 (final) with cli/v0.30.0 + kubernetes/controller/v0.29.2

Day 2: Sub-component release process
- 09:00 - Run: sub-component-release (component=cli, release_candidate=true, release_candidate_name=rc.1)
- 09:15 - CLI RC validation and testing completed
- 10:00 - Run: sub-component-release (component=cli, release_candidate=false)
         → Promotes cli/v0.30.1-rc.1 → cli/v0.30.1 (final)
- 10:01 - CI automatically triggers: ocm-release (release_candidate=true, release_candidate_name=rc.1)
- 10:02 - Creates ocm/v0.11.0-rc.1 with cli/v0.30.1 + kubernetes/controller/v0.29.2
- 10:03 - Updates /ocm/component-constructor.yaml via bot PR
- 10:05 - Runs conformance tests from /ocm/tests/

Day 3: OCM final release
- 09:00 - Run: ocm-release (release_candidate=false)
         → Promotes ocm/v0.11.0-rc.1 → ocm/v0.11.0 (final)
- 09:01 - Generate release notes linking to sub-component releases
```

### Example 2: Coordinated Multi-Component Release

```text
Day 1: Current state
- ocm/v0.11.0 (final) with cli/v0.30.1 + kubernetes/controller/v0.29.2

Day 2: First sub-component release
- 13:00 - Run: sub-component-release (component=cli, release_candidate=true, release_candidate_name=rc.1)
- 13:30 - CLI RC validation completed
- 14:00 - Run: sub-component-release (component=cli, release_candidate=false)
         → Promotes cli/v0.31.0-rc.1 → cli/v0.31.0 (final)
- 14:01 - CI automatically triggers: ocm-release (release_candidate=true, release_candidate_name=rc.1)
- 14:02 - Creates ocm/v0.12.0-rc.1 with cli/v0.31.0 + kubernetes/controller/v0.29.2

Day 3: Second sub-component release
- 09:00 - Run: sub-component-release (component=kubernetes/controller, release_candidate=true, release_candidate_name=rc.1)
- 09:30 - Controller RC validation completed
- 10:00 - Run: sub-component-release (component=kubernetes/controller, release_candidate=false)
         → Promotes kubernetes/controller/v0.30.0-rc.1 → kubernetes/controller/v0.30.0 (final)
- 10:01 - CI detects: final v0.12.0 does not exist
- 10:02 - Creates new ocm/v0.12.0-rc.2 with kubernetes/controller/v0.30.0
- 10:03 - Runs conformance tests for new RC

Day 4: OCM final release with both components
- 11:00 - Run: ocm-release (release_candidate=false)
         → Promotes ocm/v0.12.0-rc.2 → ocm/v0.12.0 (final)
- 11:01 - Final release includes: cli/v0.31.0 + kubernetes/controller/v0.30.0
- 11:02 - Generate release notes linking to both sub-component releases
- Note: ocm/v0.12.0-rc.1 remains in Git history with CLI v0.31.0 + controller/v0.29.2
```

### Example 3: `ocm` root component RC Failure and Recovery

```text
Day 1: Current state
- ocm/v0.11.0 (final) with cli/v0.30.1 + kubernetes/controller/v0.29.2

Day 2: Sub-component release triggers OCM RC
- 10:00 - cli/v0.31.0 released (final)
- 10:01 - CI automatically creates ocm/v0.12.0-rc.1 with cli/v0.31.0 + kubernetes/controller/v0.29.2
- 10:30 - OCM conformance tests FAIL (integration issue between CLI v0.31.0 and Controller v0.29.2)

Day 3: Recovery through sub-component patches
- 09:00 - Analysis identifies CLI bug in v0.31.0 affecting controller communication
- 10:00 - cli/v0.31.1 released as hotfix (direct release, no RC)
- 10:01 - CI creates new ocm/v0.12.0-rc.2 with cli/v0.31.1 + kubernetes/controller/v0.29.2
- 10:30 - OCM conformance tests PASS

Day 4: OCM final release
- 11:00 - Promote ocm/v0.12.0-rc.2 → ocm/v0.12.0 (final)
- Note: ocm/v0.12.0-rc.1 remains in Git history as failed RC with test results
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
