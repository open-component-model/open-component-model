# Transfer Configuration

* **Status**: proposed
* **Deciders**: Fabian Burth
* **Date**: 2026-04-07

**Technical Story:** Design a user-facing configuration format for the
`ocm transfer cv` command that gives users fine-grained control over the
transfer behaviour — especially over resource upload locations — while
compiling down to the existing transformation specification.

## Table of Contents

* [Context and Problem Statement](#context-and-problem-statement)
* [Decision Drivers](#decision-drivers)
* [Solution Proposal: Transfer Configuration](#solution-proposal-transfer-configuration)
  * [Overview](#overview)
  * [Resource Rules](#resource-rules)
  * [Reference Routing](#reference-routing)
  * [Multi-Component and Multi-Target Transfers](#multi-component-and-multi-target-transfers)
  * [Interaction with Existing CLI Flags](#interaction-with-existing-cli-flags)
  * [Compilation to Transformation Specification](#compilation-to-transformation-specification)
* [Considered Options](#considered-options)
* [Open Questions](#open-questions)

## Context and Problem Statement

The [transfer ADR](0003_transfer.md) established a serializable
transformation specification as the foundation for component transfers.
The [transformation ADR](0005_transformation.md) and the
[construct-as-transformation ADR](0012_construct_as_transformation.md)
further refined this into a unified, CEL-based transformation engine.

The current `ocm transfer cv` CLI command generates a transformation
specification under the hood. It provides flags like `--recursive`,
`--copy-resources` and `--upload-as` to control the generation. For
advanced use cases, the `--transfer-spec` flag allows loading a
pre-built specification from a file. The `--dry-run` flag can be
used to inspect the generated specification.

While this covers simple transfer scenarios, the current CLI has
limitations that prevent users from expressing transfers that the
underlying engine already supports:

### Limitation 1: No Control Over Resource Upload Locations

When `--upload-as ociArtifact` is used, the upload location (the OCI
image reference) of a resource is determined by a concept called
**reference name**. The reference name is the OCI repository path
and tag of the original artifact stripped of its domain part.

**Example:**
A resource originally stored at
`ghcr.io/fabianburth/my-pod:1.0.0` gets a reference name of
`fabianburth/my-pod:1.0.0`. When transferred to
`ghcr.io/target-org/ocm`, the resource ends up at
`ghcr.io/target-org/ocm/fabianburth/my-pod:1.0.0`.

This has several problems:

* **No user control.** The reference name is derived automatically from
  the source location. Users cannot choose where a resource lands in
  the target registry.
* **Cross-storage incompatibility.** The reference name assumes OCI
  naming conventions. If a resource originally stored in OCI should be
  transferred to a Maven repository, the reference name
  (`fabianburth/my-pod:1.0.0`) does not conform to Maven's `GAV`
  scheme. This is one of the problems identified in the original
  [transfer ADR](0003_transfer.md) (see "No concept to specify target
  location information for resources").
* **Local blobs without reference name.** For local blob resources,
  `--upload-as ociArtifact` silently skips resources that do not have
  a `referenceName` field set in their access specification. This is
  surprising and hard to debug.
* **Uniform strategy.** The `--upload-as` flag applies uniformly to
  all resources. There is no way to upload some resources as OCI
  artifacts and others as local blobs within the same transfer.

### Limitation 2: Single Target Repository

The CLI accepts exactly one target repository as a positional argument.
All components — including recursively discovered referenced
components — are transferred to that single target. The transfer
library already supports multiple targets per component via the
`Mapping` type and per-component resolvers. The CLI does not expose
this.

In practice, users want referenced components to end up in different
repositories. For example, shared infrastructure components should go to
a shared registry, while application-specific components go to a
team-specific registry.

### Limitation 3: Single Root Component

The CLI accepts exactly one source component reference. To transfer
multiple root components, the user has to run multiple transfers. The
transfer library already supports multiple root components via multiple
`WithTransfer` option calls.

### Summary

The underlying transfer library and transformation engine support all of
the above scenarios. The limitation is the CLI's lack of a concept for
making them configurable with a good UX. The `--transfer-spec` flag
provides an escape hatch, but writing a transformation specification by
hand is not a good UX for these scenarios.

## Decision Drivers

* **Resource upload location control** — users must be able to declare
  where each resource ends up in the target storage system. This is the
  most critical gap.
* **CEL consistency** — OCM uses CEL expressions throughout the
  transformation specification. The transfer configuration must use the
  same `${...}` expression syntax for consistency.
* **Backwards compatibility** — the existing CLI flags and positional
  arguments must continue to work for simple transfers.
* **Separation of concerns** — the transfer configuration is a
  user-facing format that compiles to a transformation specification. It
  must not leak transformation-level details (like transformation types)
  into the user-facing API.
* **Extensibility** — adding a new storage backend (e.g. Maven) must not
  require changes to the configuration format itself — only new access
  types and their fields.

## Solution Proposal: Transfer Configuration

### Overview

We introduce a YAML-based **Transfer Configuration** file. This file
declaratively describes:

1. **Which** components to transfer, from where, to where
   (`transfers` section).
2. **How** each resource should appear in the target component descriptor
   (`resourceRules` section).
3. **Where** recursively discovered referenced components should go
   (`referenceRouting` section).

The CLI receives a new `--config` flag:

```bash
# Simple transfer — unchanged
ocm transfer cv ghcr.io/src//comp:1.0.0 ghcr.io/dst

# Config-based transfer
ocm transfer cv --config transfer.yaml

# Preview the generated transformation specification
ocm transfer cv --config transfer.yaml --dry-run -o yaml
```

When `--config` is provided, positional arguments are forbidden (same
pattern as the existing `--transfer-spec` flag).

### Full Configuration Example

```yaml
apiVersion: ocm.software/v1alpha1
kind: TransferConfiguration

defaults:
  recursive: true
  copyResources: true

transfers:
  - components:
      - name: ocm.software/myapp
        version: 1.0.0
    source: ghcr.io/source-org/ocm
    target: ghcr.io/target-org/ocm

resourceRules:
  - match: "${resource.type == 'helmChart'}"
    resource:
      access:
        type: ociImage/v1
        imageReference: "${'ghcr.io/target-org/charts/' + resource.name + ':' + resource.version}"

  - match: "${access.type == 'ociImage/v1'}"
    resource:
      access:
        type: ociImage/v1
        imageReference: "${'ghcr.io/target-org/images/' + access.originalRepository + ':' + access.originalTag}"

  - match: "${true}"
    resource:
      access:
        type: localBlob/v1

referenceRouting:
  - match: "${component.name.startsWith('ocm.software/shared/')}"
    target: ghcr.io/shared-components/ocm

  - match: "${component.name.startsWith('ocm.software/infra/')}"
    target: ghcr.io/infra-team/ocm
```

### Resource Rules

Resource rules are the core of this proposal. They give users per-resource
control over **how a resource appears in the target component descriptor**
— primarily its access specification (which determines the upload
location and storage format).

#### Design Rationale

We went through several design iterations before arriving at the current
design. This section documents the rationale.

##### Iteration 1: `upload.as` + `upload.imageReference`

The first design introduced a per-resource `upload` block with an `as`
field (mirroring the `--upload-as` CLI flag) and an `imageReference`
field:

```yaml
resourceRules:
  - match: "${resource.type == 'helmChart'}"
    upload:
      as: ociArtifact
      imageReference: "${'ghcr.io/target-org/charts/' + resource.name + ':' + resource.version}"
```

This was rejected because it conflates two concerns:

1. **Which transformations to apply** — `as: ociArtifact` implies the
   compiler has to know how to convert e.g. a Helm chart to an OCI
   artifact (GetHelmChart → ConvertHelmToOCI → AddOCIArtifact). The
   conversion chain is an implementation detail that depends on the
   source access type and target access type.
2. **Where the resource ends up** — the `imageReference`.

Furthermore, `imageReference` is specific to the OCI storage backend.
A Maven upload would need `groupId`, `artifactId` etc. — the `upload`
block would have to be access-type-specific anyway.

##### Iteration 2: Explicit Transformation Pipelines

The second design allowed users to specify the entire transformation
chain:

```yaml
resourceRules:
  - match: "${resource.type == 'helmChart'}"
    transformations:
      - type: helm.transformation/getHelmChart/v1alpha1
      - type: helm.transformation/convertHelmToOCI/v1alpha1
      - type: oci.transformation/addOCIArtifact/v1alpha1
        spec:
          imageReference: "${'ghcr.io/target-org/charts/' + resource.name + ':' + resource.version}"
```

This was rejected because it leaks transformation-level implementation
details into the user-facing configuration. Most users do not want to
know about transformation types. They want to say "my Helm chart should
end up as an OCI image at this reference."

##### Iteration 3: Declare the Add Transformation

The third design asked users to declare only the final **Add
transformation** — the compiler would infer the Get and intermediate
conversions:

```yaml
resourceRules:
  - match: "${resource.type == 'helmChart'}"
    add:
      type: oci.transformation/addOCIArtifact/v1alpha1
      spec:
        imageReference: "${'ghcr.io/target-org/charts/' + resource.name + ':' + resource.version}"
```

This was better but still required users to name transformation types.
The `type: oci.transformation/addOCIArtifact/v1alpha1` is still an
implementation detail.

##### Final Design: Declare the Target Resource

The user declares the **desired state of the resource in the target
component descriptor**. The compiler figures out the transformation
chain:

```yaml
resourceRules:
  - match: "${resource.type == 'helmChart'}"
    resource:
      access:
        type: ociImage/v1
        imageReference: "${'ghcr.io/target-org/charts/' + resource.name + ':' + resource.version}"
```

The user thinks in terms of "my resource should be accessible via this
access method at this location." The compiler:

1. Looks at the source access type (e.g. `helm/v1`)
2. Looks at the desired target access type (e.g. `ociImage/v1`)
3. Finds a transformation chain that bridges them — or errors:
   "unsupported conversion from helm/v1 to ociImage/v1"

No transformation types leak into the user-facing API.

#### Rule Structure

```yaml
resourceRules:
  - match: "<cel-bool-expression>"
    resource:
      # Partial v2 resource specification.
      # Omitted fields are inherited from the source resource.
      # String values may contain ${...} CEL expressions.
      name: "..."          # optional — inherited from source if omitted
      version: "..."       # optional — inherited from source if omitted
      type: "..."          # optional — inherited from source if omitted
      relation: "..."      # optional — inherited from source if omitted
      labels:              # optional — inherited from source if omitted
        - name: "..."
          value: "..."
      extraIdentity:       # optional — inherited from source if omitted
        key: value
      access:              # required — determines storage strategy
        type: "..."        # required — target access type
        # access-type-specific fields
```

Rules are evaluated **top-down, first match wins**. If no rule matches,
the transfer falls through to the behaviour specified in `defaults`.

##### Merge Semantics

The rule's `resource` is a **partial specification**. Omitted fields
are inherited from the source resource. The merge logic:

1. Deep-copy the source resource as base.
2. Walk the rule's `resource`:
   - For each `${...}` string value: evaluate the CEL expression and
     replace with the result.
   - For each literal value: use as-is.
3. Overlay the evaluated rule values onto the base. `access` is replaced
   entirely when specified (not merged — access specs are atomic per
   type).

This means the minimal rule only needs to declare what changes:

```yaml
# Only change the upload location — everything else stays the same
- match: "${access.type == 'ociImage/v1'}"
  resource:
    access:
      type: ociImage/v1
      imageReference: "${'ghcr.io/mirror/' + access.originalRepository + ':' + access.originalTag}"
```

Full override when needed (rename, relabel, and reroute):

```yaml
- match: "${resource.name == 'legacy-db'}"
  resource:
    name: database
    version: "${resource.version}"
    labels:
      - name: migrated
        value: "true"
    access:
      type: ociImage/v1
      imageReference: "${'ghcr.io/target-org/db:' + resource.version}"
```

#### CEL Environment

Both `match` and `resource` field values use the `${...}` delimiter,
consistent with the transformation specification.

The following variables are available in the CEL environment:

| Variable | CEL Type | Description |
|---|---|---|
| `resource.name` | `string` | Resource name from descriptor |
| `resource.version` | `string` | Resource version from descriptor |
| `resource.type` | `string` | Resource type (e.g. `ociImage`, `helmChart`) |
| `resource.relation` | `string` | `"local"` or `"external"` |
| `resource.labels` | `list(map(string,dyn))` | Resource labels |
| `resource.extraIdentity` | `map(string,string)` | Extra identity attributes |
| `component.name` | `string` | Owning component name |
| `component.version` | `string` | Owning component version |
| `access.type` | `string` | Source access type (e.g. `localBlob/v1`, `ociImage/v1`) |
| `access.mediaType` | `optional(string)` | Media type (LocalBlob only) |
| `access.referenceName` | `optional(string)` | Reference name (LocalBlob only) |
| `access.imageReference` | `optional(string)` | Full image ref (OCIImage only) |
| `access.originalDomain` | `optional(string)` | Registry domain parsed from imageReference |
| `access.originalRepository` | `optional(string)` | Repository path without domain |
| `access.originalTag` | `optional(string)` | Tag parsed from imageReference |
| `access.helmRepository` | `optional(string)` | Helm repo URL (Helm only) |
| `access.helmChart` | `optional(string)` | Helm chart name (Helm only) |
| `target.baseUrl` | `string` | Target registry base URL |
| `target.subPath` | `optional(string)` | Target registry sub-path |

The `match` expression must evaluate to `bool`.
String values in `resource` are either literal strings or `${...}`
CEL expressions evaluating to the appropriate type.

Optional types (already supported in the CEL environment via
`cel.OptionalTypes()`) ensure that `access.imageReference` is only
present for OCI access types — no runtime errors when matching across
access types.

#### Go Types

The resource rule type reuses the existing `v2.Resource` type from the
descriptor package, which already uses `*runtime.Raw` for the `Access`
field — the same unstructured representation used throughout the
transformation specification:

```go
// ResourceRule defines a per-resource routing and transformation policy.
type ResourceRule struct {
    // Match is a CEL expression (${...} delimited) evaluating to bool.
    Match string `json:"match"`

    // Resource is a partial v2 resource specification.
    // Omitted fields are inherited from the source resource.
    // String values may contain ${...} CEL expressions.
    Resource *descriptorv2.Resource `json:"resource"`
}
```

Using `*descriptorv2.Resource` directly avoids inventing a new type.
The `Access` field (`*runtime.Raw`) handles arbitrary access
specifications. Validation of required fields is skipped for the rule's
resource — it is a partial specification, not a complete descriptor
entry.

New option for the transfer library:

```go
func WithResourceRules(rules []ResourceRule) Option {
    return func(o *Options) {
        o.ResourceRules = rules
    }
}
```

#### The Compiler: Conversion Matrix

The compiler maintains a registry of supported conversions:

```
(source access type) → (target access type) = transformation chain
```

The initial matrix:

| Source | Target `ociImage/v1` | Target `localBlob/v1` |
|---|---|---|
| `localBlob/v1` | Get → AddOCIArtifact | Get → AddLocalResource |
| `ociImage/v1` | Get → AddOCIArtifact | Get → AddLocalResource |
| `helm/v1` | Get → ConvertToOCI → AddOCIArtifact | Get → ConvertToOCI → AddLocalResource |

When a new storage backend is added (e.g. Maven), a new column is added
to the matrix with the required conversion transformations. The
configuration format does not change — users write
`type: maven/v1` with the appropriate fields (`groupId`,
`artifactId`, `version`, `repository`).

If a conversion is not supported, the compiler returns a clear error:

```
resource "my-resource" (source access type helm/v1):
  conversion to target access type maven/v1 is not supported
```

#### CEL Compilation Strategy

Resource rule CEL expressions are compiled **once** during
`BuildGraphDefinition`, not per-resource. The CEL environment (variable
declarations) is the same for all resources — only the activation
record (variable values) changes per resource.

```
BuildGraphDefinition
  ├─ compile all rule.Match expressions → []cel.Program   (once)
  ├─ compile all rule's ${...} expressions  → []cel.Program (once)
  │
  └─ fillGraphDefinitionWithPrefetchedComponents
       └─ processResources
            └─ for each resource:
                 evaluate pre-compiled programs with resource-specific activation
```

#### How `imageReference` Evaluation Works

The rule's `imageReference` CEL is evaluated at **graph construction
time** (when we have the descriptor data), not at graph execution time.
The result is a concrete string like
`"ghcr.io/target-org/charts/mariadb:12.2.7"` that is embedded in the
AddOCIArtifact transformation spec.

This is different from the current hardcoded
`${getResourceID.output.resource.access.referenceName}` pattern, which
is evaluated at execution time. The key insight: for rule-matched
resources, we already know the upload location at graph construction time
because we have the full descriptor.

**Example: Helm Chart**

Source resource:
```yaml
name: my-chart
version: 12.2.7
type: helmChart
relation: external
access:
  type: helm/v1
  helmRepository: https://charts.bitnami.com/bitnami
  helmChart: mariadb
  version: 12.2.7
```

Rule:
```yaml
- match: "${resource.type == 'helmChart'}"
  resource:
    labels:
      - name: migrated
        value: "true"
    access:
      type: ociImage/v1
      imageReference: "${'ghcr.io/target-org/charts/' + resource.name + ':' + resource.version}"
```

After CEL evaluation and merge, the target resource is:
```yaml
name: my-chart              # inherited from source
version: 12.2.7             # inherited from source
type: helmChart             # inherited from source
relation: external          # inherited from source
labels:                     # overridden by rule
  - name: migrated
    value: "true"
access:                     # overridden by rule (CEL evaluated)
  type: ociImage/v1
  imageReference: ghcr.io/target-org/charts/my-chart:12.2.7
```

The compiler:
1. Sees source access `helm/v1` → target access `ociImage/v1`.
2. Looks up the conversion chain: GetHelmChart → ConvertHelmToOCI →
   AddOCIArtifact.
3. Emits transformations with `imageReference:
   "ghcr.io/target-org/charts/my-chart:12.2.7"`.

The user specified the full `imageReference` using descriptor-level data
(`resource.name`, `resource.version`). No dependency on conversion
output.

#### What This Replaces

The **reference name concept** is no longer needed as the mechanism for
determining upload locations. Today:

- `localblob.go` hardcodes
  `imageReference: targetRepoBaseURL/${referenceName}`
- `oci.go` does the same via `staticReferenceName()`

With resource rules, the `imageReference` in the AddOCIArtifact
transformation spec comes from the rule's evaluated CEL expression.
The reference name may still appear in the CEL environment as
`access.referenceName` for users who want to reference it, but it is no
longer the implicit upload path.

### Reference Routing

During recursive discovery, child components currently inherit the root's
single target. Reference routing lets users split referenced components
across different registries.

```yaml
referenceRouting:
  - match: "${component.name.startsWith('ocm.software/shared/')}"
    target: ghcr.io/shared-components/ocm

  - match: "${component.name.startsWith('ocm.software/infra/')}"
    target: ghcr.io/infra-team/ocm
```

Rules are evaluated top-down, first match wins. Unmatched children fall
through to the root's target.

The CEL environment provides:

| Variable | CEL Type | Description |
|---|---|---|
| `component.name` | `string` | Referenced component name |
| `component.version` | `string` | Referenced component version |
| `parent.name` | `string` | Parent component name |
| `parent.version` | `string` | Parent component version |

During the discovery phase, when a child component is found via a
component reference, the child is matched against `referenceRouting`
rules. A match overrides the inherited target with the rule's target.

### Multi-Component and Multi-Target Transfers

The `transfers` section supports multiple root components and multiple
targets:

```yaml
transfers:
  # Multiple components to the same target
  - components:
      - name: ocm.software/frontend
        version: 2.0.0
      - name: ocm.software/backend
        version: 3.1.0
    source: ghcr.io/source-org/ocm
    target: ghcr.io/target-org/ocm

  # Same component to multiple targets (fan-out)
  - components:
      - name: ocm.software/myapp
        version: 1.0.0
    source: ghcr.io/source-org/ocm
    targets:
      - ghcr.io/target-a/ocm
      - ghcr.io/target-b/ocm
```

Each entry maps to a `transfer.WithTransfer()` call in the transfer
library.

### Interaction with Existing CLI Flags

| Scenario | Behaviour |
|---|---|
| No `--config` | Today's behaviour. Positional args + flags. |
| `--config transfer.yaml` | Config file drives the transfer. Positional args forbidden. |
| `--config` + `--dry-run` | Config-based generation, output the transformation spec. |
| `--config` + `--recursive` changed | Warning: flag ignored, config's `defaults` takes precedence. |

Global defaults in the config (`defaults.recursive`,
`defaults.copyResources`) subsume the `--recursive` and
`--copy-resources` flags. When `resourceRules` is present, it takes
precedence over `defaults.uploadAs`.

When `resourceRules` is absent, `defaults.uploadAs` behaves identically
to today's `--upload-as` flag.

### Compilation to Transformation Specification

The transfer configuration compiles to a transformation specification
before execution. The pipeline:

```
TransferConfiguration (YAML)
    │ parse
    ▼
TransferConfiguration (Go struct)
    │ compile
    │ - resolve transfers → transfer.WithTransfer() options
    │ - evaluate resourceRules CEL → per-resource target state
    │ - look up conversion matrix → transformation chains
    │ - resolve referenceRouting → target overrides during discovery
    ▼
transfer.BuildGraphDefinition(...)
    │
    ▼
TransformationGraphDefinition
    │ build + execute (or --dry-run)
    ▼
Result
```

The resource rules are applied during graph construction phase 2
(`processResource`). Instead of the current hardcoded
`staticReferenceName()` / `imageReferenceFromAccess()`, the rule engine
evaluates the matching rule's target access spec to produce the concrete
values in the Add transformation.

## Considered Options

### Option 1: Transfer Configuration (this proposal)

A declarative YAML configuration file that compiles to a transformation
specification.

**Pro:**
* Clean separation between user intent (what the target should look
  like) and implementation (which transformations are needed).
* CEL consistency with the rest of OCM.
* Backwards compatible — existing flags continue to work.
* Extensible — new storage backends do not require format changes.

**Con:**
* Additional compilation step between config and transformation spec.
* Users who need full transformation-level control must still use
  `--transfer-spec`.

### Option 2: Extend CLI Flags

Add more flags to the existing command: `--resource-rule`, `--target-for`,
`--reference-route`, etc.

**Pro:**
* No config file needed for moderate complexity.

**Con:**
* Flag combinatorics become unmanageable.
* Per-resource rules with CEL expressions do not fit well into CLI flags.
* No good way to express partial resource specs as flag values.

### Option 3: Write Transfer Specs by Hand

Rely on `--transfer-spec` for all advanced use cases.

**Pro:**
* No new format to maintain.
* Full power of the transformation engine.

**Con:**
* Writing a transformation spec is extremely verbose (see
  [construct-as-transformation ADR](0012_construct_as_transformation.md)
  — even a simple transfer produces many transformations).
* Requires knowledge of transformation types and CEL data flow between
  them.
* Error-prone — the spec is a low-level representation not designed for
  hand-authoring.

## Implementation Phases

| Phase | Scope | Priority |
|---|---|---|
| **1** | `resourceRules` — per-resource upload location and access type control. Replaces reference name dependency. | Critical |
| **2** | `referenceRouting` — target resolver for recursive transfers. | High |
| **3** | Multi-component + multi-target `transfers` section. | Medium |
| **4** | Extended `match` conditions (labels, extra identity). Broader CEL variable set. | Lower |

Phase 1 alone solves the most critical problem. The configuration format
is forward-compatible with phases 2-4 without breaking changes.

## Open Questions

* **Resource rules and multiple targets:** When a component goes to
  multiple targets (phase 3), should resource rules be evaluated
  per-target (so the same resource can go to different locations
  depending on the target)? Per-target seems correct since
  `target.baseUrl` is in the CEL environment, but this needs
  confirmation.
* **Source repository in CEL environment:** Is matching on
  `resource.*` + `access.*` + `component.*` + `target.*` sufficient,
  or do we need to match on the source repository too (e.g. "resources
  coming from ghcr.io get routed differently than those from quay.io")?
