# ADR 0009 — Versioning, Branching and Releasing for the OCM Monorepo

* Status: Proposed
* Deciders: OCM Technical Steering Committee
* Date: 2025-09-16

Technical Story: Define a single, operational specification for component versioning, release-branching, and CI publishing for the OCM monorepo using Git tags as the single source of truth.

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

## Proposal

We propose a Git-native versioning strategy that eliminates version files in favor of authoritative Git tags as the single source of truth. This approach combines SemVer-compliant Git tag-based versioning for individual sub-components with component-scoped persistent release branches for maintenance workflows, while leveraging an OCM Component Constructor YAML for managing the root OCM component's version matrix and coordinated releases

The strategy includes dedicated conformance testing at the OCM root level to ensure component interoperability and validate integrated component combinations before final releases. The tests will be created over time and are not mandatory for the initial rollout of this strategy.

## Description

We adopt a Git-native, tag-based versioning strategy that eliminates version files in favor of authoritative Git tags:

* **Versioning**: Git tags are the single source of truth for component versions - no VERSION files are maintained in the repository
* **Branching**: Component-scoped persistent release branches following the pattern `releases/<component>/v<major>.<minor>` for maintenance lines
* **Publishing**: Annotated Git tags with Go module-compatible naming (`<component>/v<major>.<minor>.<patch>`) serve as the authoritative record of published artifacts
* **OCM Root Component**: Uses OCM Component Constructor YAML as version matrix and releases directly from `main` branch without release branches
* **Integration Validation**: Dedicated conformance tests at the OCM root component level validate sub-component interoperability and ensure tested combinations before final releases

## High-level Architecture

All component versions are determined exclusively from Git tags:

```text
Repository Structure:
├── cli/                     # OCM CLI component
├── kubernetes/controller/   # OCM Controller component  
├── ocm/                     # OCM root component
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

OCM root component releases directly from `main` branch with no release branches.

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

Sub-components follow a Release Candidate (RC) workflow similar to OCM releases:

**Release Candidate Creation**:

* **Trigger**: Manual workflow dispatch when ready to release from release branch
* **Purpose**: Create RC for testing and validation before final release

**Steps**:

1. **Pre-flight Checks**: Validate branch naming, component existence, and readiness
2. **Version Calculation**: Determine next version from existing tags on the branch
3. **Testing**: Run component-specific tests and verification
4. **Build & Publish**: Create artifacts (binaries, container images)
5. **Component Version Creation**: Generate and publish OCM component in ghcr.io
6. **RC Tag Creation**: Create annotated Git tag `<component>/v<major>.<minor>.<patch>-rc.1`
7. **RC Validation**: Extended testing period for release candidate
8. **Documentation**: Prepare release documentation and notes

**Final Release Promotion**:

* **Trigger**: Manual workflow dispatch after RC validation
* **Purpose**: Promote validated RC to final release
* **Steps**:
  1. **RC Verification**: Confirm RC exists and validation completed
  2. **Final Tag Creation**: Create final tag `<component>/v<major>.<minor>.<patch>`
  3. **OCM Trigger**: Automatically trigger OCM root component release candidate creation
  4. **GitHub Release**: Create GitHub release with component-specific release notes
  5. **Artifact Publishing**: Publish final artifacts to registries

#### Hotfix/Backport Procedure

**Process**:

1. Implement fix on the appropriate `releases/<component>/vX.Y` branch
2. Open PR targeting the release branch
3. After merge, backport change to `main` branch (cherry-pick or separate PR)

**Example**: Hotfix for CLI v0.30.2 goes to `releases/cli/v0.30` → `cli/v0.30.2`

#### Version Determination Logic

Sub-components use Git tags exclusively for version management. A possible script snippet for determining the next patch version on a release branch could look like this:

```bash
# For new releases on release branch
LATEST_PATCH=$(git tag -l "${COMPONENT}/v${MAJOR}.${MINOR}.*" | sort -V | tail -1)
if [ -z "$LATEST_PATCH" ]; then
  NEXT_VERSION="${MAJOR}.${MINOR}.0"  # First release on branch
else
  PATCH_NUM=$(echo $LATEST_PATCH | cut -d. -f3)
  NEXT_VERSION="${MAJOR}.${MINOR}.$((PATCH_NUM + 1))"  # Increment patch
fi
```

### OCM Root Component Strategy

The OCM root component utilizes an **OCM Component Constructor YAML** located in `/ocm/component-constructor.yaml` as the authoritative version matrix. This constructor:

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

### OCM Release Process (Simplified)

**Key Principle**: Any sub-component release of type **patch** and **minor** triggers an OCM **minor** version bump, as each represents a new combination of tested components. A **major** release of a sub-component triggers an OCM **major** version bump.

**Versioning Logic**:

* Sub-component change: patch/minor → OCM: MINOR bump
* Rationale: Each OCM release represents a new tested combination of sub-components

**Release Workflow**:

1. Sub-component releases and creates tag (e.g., `cli/v0.30.1`)
2. Check for existing OCM release candidate for next version
3. If RC exists and no final release: Update existing RC with new sub-component versions
4. If no RC exists: Create new RC (e.g., `ocm/v0.11.0-rc.1`)
5. Run integration conformance tests from `/ocm/tests/` (if available)
6. Manual promotion: RC → Final release (e.g., `ocm/v0.11.0`)

**No OCM Release Branches**: OCM releases are created directly from `main` branch since OCM is a snapshot-only component with no independent development lifecycle.

### OCM Integration Testing

The `/ocm/` directory contains:

* **Component Constructor**: `/ocm/component-constructor.yaml` - defines sub-component versions and relationships
* **Conformance Tests**: `/ocm/tests/` - integration tests covering scenarios where multiple components interact. These tests validate that the specific combination of sub-component versions works together as expected. They will be created over time and are not mandatory for the initial rollout of this strategy.
* **Test Scenarios**: End-to-end workflows validating component interoperability beyond individual component tests

These conformance tests are executed during the RC-to-final promotion process to ensure component compatibility.

### Release Notes Strategy

OCM releases use simplified aggregated release notes that reference existing sub-component releases rather than duplicating content:

```text
## 📦 Included Components

- **OCM CLI**: [`v0.30.1`](link-to-cli-release)
- **OCM Controller**: [`v0.29.3`](link-to-controller-release)

---
*This OCM release bundles the above tested and compatible component versions.*
```

This approach leverages existing sub-component release notes while maintaining traceability.

### Component Constructor as Version Matrix

The OCM root component utilizes an **OCM Component Constructor YAML** located in `/ocm/component-constructor.yaml` as the authoritative version matrix. This constructor:

* **References exact sub-component versions**: Maps to specific Git tags (e.g., cli/v0.30.1)
* **Enables reproducible builds**: Locks OCM to tested component combinations
* **Supports partial updates**: OCM releases can include subset of sub-components with new versions
* **Single source of truth**: Eliminates version conflicts between components

## CI/CD Workflows and Automation

The release strategy is implemented through several automated workflows that handle different aspects of the release process:

### Workflow: `create-release-branch`

**Trigger**: Manual workflow dispatch OR automated trigger when preparing new minor release

**Purpose**: Initialize a new persistent release branch for a component's minor version line

**Required Steps**:

1. **Branch validation**: Verify that `releases/<component>/v<major>.<minor>` doesn't already exist
2. **Source determination**: Create branch from `main` for new minor version release
3. **Branch creation**: Create `releases/<component>/v<major>.<minor>` branch using OCM bot
4. **Protection rules**: Apply branch protection (require PR reviews, status checks)
5. **Notification**: Alert component maintainers of new release branch creation

**Note**: OCM root component does not require release branches - it uses tags directly from `main` branch.

### Workflow: `sub-component-release`

**Trigger**: Manual workflow dispatch

**Purpose**: Handle both release candidate creation and promotion to final release for sub-components

**Inputs**:

* `component`: Component to release (e.g., "cli", "controller")
* `release_candidate`: Boolean - true for RC creation, false for final release promotion
* `release_candidate_name`: String - RC suffix (e.g., "rc.1", "rc.2") when creating RCs

**Required Steps**:

**For Release Candidate Creation** (`release_candidate: true`):

1. **Pre-flight checks**: Validate branch naming convention and component existence
2. **Version calculation**: Determine next patch version from existing tags on branch
3. **Component testing**: Run component-specific tests and verification
4. **Artifact creation**: Build and publish RC artifacts (binaries, container images, component descriptors)
5. **RC tag creation**: Create annotated tag `<component>/vX.Y.Z-rc.N` using OCM bot
6. **RC validation**: Trigger extended validation and testing workflows
7. **Notification**: Alert component maintainers of RC availability for validation

**For Final Release Promotion** (`release_candidate: false`):

1. **RC validation**: Verify that latest RC exists and validation completed
2. **Final tag creation**: Create annotated tag `<component>/vX.Y.Z` using OCM bot (from RC commit)
3. **Final artifact publishing**: Publish final artifacts to registries
4. **OCM Integration**:
   * Check for existing OCM release candidate for next minor version
   * If RC exists and no final release: Log that RC will include new sub-component version
   * If no RC exists: Create new OCM RC with updated component constructor
   * Run conformance tests from `/ocm/tests/` for the RC (if available)
5. **GitHub Release**: Create GitHub release with component-specific release notes
6. **Notification**: Alert maintainers of successful sub-component release
7. **Failure handling**: If any step fails, stop pipeline and alert maintainers

### Workflow: `ocm-release`

**Trigger**: Manual workflow dispatch OR automated trigger from sub-component releases

**Purpose**: Handle both OCM release candidate creation and promotion to final release

**Inputs**:

* `release_candidate`: Boolean - true for RC creation, false for final release promotion
* `release_candidate_name`: String - RC suffix (e.g., "rc.1", "rc.2") when creating RCs

**Required Steps**:

**For Release Candidate Creation** (`release_candidate: true`):

1. **Version calculation**: Determine next OCM minor version based on sub-component changes
2. **Component constructor update**: Update `/ocm/component-constructor.yaml` with latest sub-component versions
3. **RC tag creation**: Create annotated tag `ocm/vX.Y.Z-rc.N` directly from `main`
4. **Integration testing**: Run conformance tests from `/ocm/tests/` (if available)
5. **Notification**: Alert maintainers of OCM RC availability for validation

**For Final Release Promotion** (`release_candidate: false`):

1. **RC validation**: Verify that specified RC exists and conformance tests have passed
2. **Version resolution**: Resolve final sub-component versions at promotion time (latest available versions)
3. **Component constructor update**: Update `/ocm/component-constructor.yaml` with final component versions
4. **Tag creation**: Create annotated tag `ocm/vX.Y.Z` directly from `main`
5. **Release notes generation**: Generate simplified release notes linking to sub-component releases
6. **Artifact publishing**: Publish OCM artifacts and update documentation
7. **Notification**: Alert maintainers of successful OCM release

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
* **No version files**: Repository contains no VERSION files - all versions derived from Git tags

### Release Branch Lifecycle

* **Creation**: Release branches created from `main` when starting new minor version development
* **Maintenance**: Persistent branches maintained for active minor versions requiring patches
* **Protection**: Branch protection rules prevent direct pushes and require PR reviews
* **Archival**: Old release branches archived when no longer maintained (policy TBD)

### Integration with OCM Releases

Every sub-component release automatically triggers OCM release candidate evaluation:

1. **Version impact assessment**: Determine if new OCM RC needed or existing RC updated
2. **Component constructor update**: Automatic PR to update component references
3. **Integration testing**: Conformance tests validate component combinations
4. **Manual promotion gate**: Human approval required for final OCM releases

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
- 10:01 - CI detects: existing RC ocm/v0.12.0-rc.1 exists, no final v0.12.0
- 10:02 - No new RC created - existing RC will include updated controller version
- 10:03 - Log: "RC will include kubernetes/controller/v0.30.0 when finalized"

Day 4: OCM final release with both components
- 11:00 - Run: ocm-release (release_candidate=false)
         → Promotes ocm/v0.12.0-rc.1 → ocm/v0.12.0 (final)
- 11:01 - Final release includes: cli/v0.31.0 + kubernetes/controller/v0.30.0
- 11:02 - Generate release notes linking to both sub-component releases
```

## Pros and Cons of the Proposal

Pros:

* **Simplicity**: Git tags are Git-native and require no additional file maintenance
* **Auditability**: Complete version history is preserved in Git repository
* **Go Module Compatibility**: Tag naming follows Go module conventions exactly
* **Reduced Merge Conflicts**: No version files to conflict during parallel releases
* **Single Source of Truth**: Git tags eliminate version drift between files and actual releases
* **OCM Native**: Component Constructor used as version matrix follows OCM specification patterns

Cons:

* **Tag Dependency**: CI/CD pipelines must be robust in Git tag parsing and validation
* **Learning Curve**: Teams need to understand Git tagging workflows for version management

## Conclusion

This Git tag-based versioning strategy with OCM Component Constructor provides a robust, Git-native approach to monorepo component versioning and releasing. By eliminating version files and using Git tags as the single source of truth, we reduce maintenance overhead while ensuring full compatibility with Go module conventions and OCM specifications.

The OCM root component serves as a tested integration point for sub-components, providing users with verified compatible component combinations while maintaining the flexibility for independent sub-component releases.
