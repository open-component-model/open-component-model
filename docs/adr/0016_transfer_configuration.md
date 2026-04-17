# Transfer Configuration

* **Status**: proposed
* **Deciders**: SIG Runtime
* **Date**: 2026-04-07

**Technical Story:** Design a user-facing configuration format for the
`ocm transfer cv` command that gives users fine-grained control over the
transfer behaviour — especially over resource upload locations — while
compiling down to the existing transformation specification.

## Table of Contents

* [Context and Problem Statement](#context-and-problem-statement)
* [Decision Drivers](#decision-drivers)
* [Considered Options](#considered-options)
* [Decision Outcome](#decision-outcome)
* [Pros and Cons of the Options](#pros-and-cons-of-the-options)
* [Implementation Phases](#implementation-phases)
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
* **Simplicity** — the configuration must be easy to write, read, and
  understand without deep knowledge of OCM internals or expression
  languages.
* **Versionability** — the configuration format must be easy to evolve
  independently of the transformation specification. Typed, narrowly
  scoped configurations are straightforward to version (`v1alpha1` →
  `v1beta1` → `v1`) without breaking consumers.
* **Backwards compatibility** — the existing CLI flags and positional
  arguments must continue to work for simple transfers.
* **Separation of concerns** — the transfer configuration is a
  user-facing format that compiles to a transformation specification. It
  must not leak transformation-level details (like transformation types)
  into the user-facing API.

## Considered Options

* **Option 1:** Typed Transfer Configurations (dedicated, narrowly scoped
  config types per use case)
* **Option 2:** Generic Transfer Configuration (single CEL-based config
  with match/template rules)

## Decision Outcome

Chosen [Option 1](#option-1-typed-transfer-configurations): "Typed
Transfer Configurations".

Justification:

* We are not yet confident in what exactly customers need from transfer
  configuration. Starting with the simplest possible solution lets us
  ship quickly, gather real usage feedback, and evolve the configuration
  surface based on actual demand rather than speculation.
* Covers the most critical transfer customisation need (controlling
  where resources end up) without requiring users to learn CEL or
  understand the transformation engine.
* Each config type is small and independently versionable. If a type
  turns out to be wrong, it can be deprecated without affecting the
  others.
* Implementation complexity is low — each config type is a thin compiler
  to the existing transformation specification.

## Option 1: Typed Transfer Configurations

### Description

Instead of a single, generic configuration format, we introduce
**dedicated typed configuration resources** — one per use case. Each
type has a narrow functional scope, is independently versioned, and
compiles to transformation specification primitives.

This ADR focuses on the most critical feature: per-resource control
over upload locations. Multi-component transfers, multi-target
transfers, and reference routing are out of scope for the initial
implementation and will be addressed by future typed configs.

The CLI receives a new `--config` flag:

```bash
# Simple transfer — unchanged
ocm transfer cv ghcr.io/src//comp:1.0.0 ghcr.io/dst

# Config-based transfer
ocm transfer cv --config transfer.yaml \
    ghcr.io/src//comp:1.0.0 ghcr.io/dst

# Preview the generated transformation specification
ocm transfer cv --config transfer.yaml --dry-run -o yaml \
    ghcr.io/src//comp:1.0.0 ghcr.io/dst
```

The positional source and target arguments remain required.

### Single-File Configuration

All typed configs are bundled in a single file using the existing
[generic configuration](../../bindings/go/configuration/generic/v1/spec/config.go)
wrapper (`generic.config.ocm.software/v1`). This is the same pattern
used for OCM resolver and HTTP client configuration:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: OCIImageReferenceOverride/v1alpha1
    spec:
      - resource:
          name: my-pod
        oci:
          registry: ghcr.io
          repository: target-org/images/my-pod
          tag: "1.0.0"
```

At load time, the generic config is parsed via the existing
`configuration` package. Each entry is deserialized into its concrete
Go type via `runtime.Scheme`. The transfer command collects all
recognized transfer config types from the generic wrapper and feeds
them to the compiler.

### OCIImageReferenceOverride

Allows users to declare target image references for specific resources,
regardless of the source access type (OCI, Helm, local blob, etc.).
The compiler infers the appropriate transformation chain based on the
source access type of each matched resource. This directly addresses
[Limitation 1](#limitation-1-no-control-over-resource-upload-locations).

#### Example

```yaml
type: OCIImageReferenceOverride/v1alpha1
spec:
  # Full override — resource in the root component
  - resource:
      name: my-pod
    oci:
      registry: ghcr.io
      repository: target-org/images/my-pod
      tag: "1.0.0"

  # Partial override — only registry and repository, tag preserved from source.
  # A resource originally at myrepo.org/project/my-sidecar:2.1.0 would be
  # relocated to ghcr.io/target-org/images/my-sidecar:2.1.0.
  - resource:
      name: my-sidecar
    oci:
      registry: ghcr.io
      repository: target-org/images/my-sidecar

  # Resource in a referenced component, disambiguated by referencePath
  - referencePath:
      - name: db-stack
    resource:
      name: monitoring-agent
      platform: linux/amd64
    oci:
      registry: ghcr.io
      repository: target-org/images/monitoring
      tag: "2.3.1"

  # Helm chart — compiler infers GetHelm → ConvertToOCI → AddOCIArtifact
  - resource:
      name: mariadb
    oci:
      registry: ghcr.io
      repository: target-org/charts/mariadb
      tag: "12.2.7"
```

The `oci` field is always an object. When `tag` is omitted, the
tag is preserved from the source — this avoids updating the config on
every version bump.

#### Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `spec[].referencePath` | `[]Identity` | no | Path of component reference identities from the root component to the component that owns the resource. Empty or omitted for resources in the root component. |
| `spec[].resource.name` | `string` | yes | Resource name |
| `spec[].resource.<key>` | `string` | no | Extra identity attributes as additional keys (for disambiguation when multiple resources share a name) |
| `spec[].oci` | `object` | yes | Target OCI image reference. |
| `spec[].oci.registry` | `string` | no | Target registry (e.g. `ghcr.io`). |
| `spec[].oci.repository` | `string` | no | Target repository path (e.g. `target-org/images/my-pod`). |
| `spec[].oci.tag` | `string` | no | Target tag (e.g. `1.0.0`). When omitted, the tag is preserved from the source. |

#### Field Defaults

All `oci` fields are optional. When a field is omitted, the compiler
resolves a default based on the source access type:

| Field | Source: `ociImage` | Source: `localBlob` | Source: `helm` |
|---|---|---|---|
| `registry` | Transfer target registry | Transfer target registry | Transfer target registry |
| `repository` | Original repository from source access | **Error** — local blobs have no repository; field is required | **Error** — the Helm-to-OCI conversion (GetHelmChart → ConvertHelmToOCI → AddOCIArtifact) does not produce a default repository path; field is required |
| `tag` | Original tag from source access | Resource version from the component descriptor | Chart version from the Helm chart metadata (available after GetHelmChart) |

#### Resource Identity

A resource identity is represented as a flat map: `name` is always
required, and any additional keys are extra identity attributes. This
flattened form avoids the nesting of a separate `extraIdentity` map:

```yaml
resource:
  name: monitoring-agent
  platform: linux/amd64
```

This is equivalent to a resource with `name: monitoring-agent` and
`extraIdentity: {platform: linux/amd64}` in the component descriptor.

A resource identity is only unique within a single component descriptor.
During a recursive transfer, the same component may appear at multiple
positions in the component tree (even in different versions). The
`referencePath` traces the path of component references from the root
to the owning component, making each entry globally unambiguous. This
follows the same addressing scheme as the controller's
[`ResourceReference`](../../kubernetes/controller/api/v1alpha1/common_types.go)
type. For resources in the root component, `referencePath` is omitted.
Each entry addresses exactly one resource — there is no pattern
matching or globbing.

#### Semantics

* Each entry identifies a resource by its reference path and resource
  identity, and declares the target OCI reference it should be
  uploaded to. Omitted fields are preserved from the source.
* The compiler looks at the source access type of the matched resource
  and infers the appropriate transformation chain:
  * `ociImage/v1` → Get → AddOCIArtifact
  * `helm/v1` → GetHelmChart → ConvertHelmToOCI → AddOCIArtifact
  * `localBlob/v1` → Get → AddOCIArtifact
  * Unsupported source type → clear error
* For `helm/v1` sources, the `repository` field is required because
  the Helm-to-OCI conversion chain does not preserve a source
  repository path — the original Helm chart reference (chart name +
  repo URL) has no direct OCI repository equivalent. The `tag` field
  defaults to the chart version from the Helm chart metadata, which
  is available after the GetHelmChart step in the conversion chain.
  The `registry` field follows the same default as all other source
  types (the transfer target registry).
* Resources not matched by any override entry fall through to the
  default behaviour (as determined by `--upload-as` / `--copy-resources`
  flags).
* Duplicate entries (same reference path + resource identity appearing
  more than once) are rejected at parse time.

#### Go Types

```go
// OCIImageReferenceOverrideConfig declares target image references
// for specific resources, regardless of source access type.
type OCIImageReferenceOverrideConfig struct {
    runtime.Type `json:",inline"`

    Spec []OCIImageReferenceOverride `json:"spec"`
}

type OCIImageReferenceOverride struct {
    ReferencePath  []runtime.Identity `json:"referencePath,omitempty"`
    Resource       runtime.Identity   `json:"resource"`
    OCI            OCIReference       `json:"oci"`
}

// OCIReference specifies the target OCI image location.
// All fields are optional — omitted fields are preserved from the source.
type OCIReference struct {
    // Registry is the target registry (e.g. "ghcr.io").
    Registry string `json:"registry,omitempty"`
    // Repository is the repository path (e.g. "org/repo").
    Repository string `json:"repository,omitempty"`
    // Tag is the image tag (e.g. "1.0.0"). When omitted, preserved from source.
    Tag string `json:"tag,omitempty"`
}
```

The `Resource` field uses `runtime.Identity` (which is
`map[string]string`) directly. The `name` key is always required.
Additional keys are extra identity attributes.

### Interaction with Existing CLI Flags

| Scenario | Behaviour |
|---|---|
| No `--config` | Today's behaviour. Positional args + flags. |
| `--config transfer.yaml` | Config entries override matching resources. Non-matching resources follow flag behaviour. |
| `--config` + `--dry-run` | Config-based generation, output the transformation spec. |

### Compilation to Transformation Specification

The config is loaded once and indexed by resource identity before graph
construction begins. During `processResource` — the existing
per-resource loop inside `BuildGraphDefinition` — each resource is
checked against the index. If a matching override entry exists, the
compiler infers the transformation chain from the source access type
and emits it with the configured `oci` target. Otherwise, the
resource falls through to the default behaviour.

```text
generic.config.ocm.software/v1 (YAML)
    │ parse via configuration package
    ▼
[]runtime.Raw
    │ deserialize each entry by type
    ▼
OCIImageReferenceOverride
    │ index by (referencePath + resource identity)
    ▼
transfer.BuildGraphDefinition(...)
    │
    └─ processResource (for each resource):
         │ look up resource in override index
         │
         ├─ match found → infer chain from source access type
         │   emit transformation chain with oci target
         │
         └─ no match → default behaviour
              (--upload-as / --copy-resources)
```

### Adding New Typed Configurations

Additional typed configurations are introduced on demand — only when the
corresponding feature is requested. For example, reference routing for
recursive transfers (Phase 2) and multi-component / multi-target
transfers (Phase 3) would each get their own typed config if and when
users need them. Until then, they are not implemented.

When a new use case arises, a new typed configuration is introduced:

1. Define a new type (e.g. `ReferenceRouting`,
   `MultiTargetTransfer`).
2. Register it in the `runtime.Scheme` so the generic config wrapper
   can deserialize it.
3. Implement a compiler from the new type to transformation
   specification primitives.
4. Version independently (`v1alpha1` → `v1`).

The existing typed configs remain unchanged. Users add a new entry to
their `configurations` list — no combinatorial explosion of fields in a
single format.

## Pros and Cons of the Options

### Option 1: Typed Transfer Configurations

**Pros:**

* **Simple to understand.** Each config type does one thing. A user who
  needs image reference relocation reads only
  `OCIImageReferenceOverride` — no CEL, no match
  expressions, no template syntax.
* **Easy to version.** Each type evolves on its own lifecycle.
* **Low implementation complexity.** Each compiler is a small, focused
  function. No generic CEL evaluation pipeline, no expression
  compilation, no partial-merge engine.
* **Easy to validate.** Literal values can be validated at parse time
  (e.g. "is this a valid OCI image reference?"). No runtime expression
  evaluation surprises.
* **Backwards compatible.** Existing flags and positional arguments
  continue to work. Configs only override matching resources.
* **Composable.** Multiple typed configs coexist in a single file via
  the existing generic config wrapper. Each typed entry is
  self-contained and independently versionable.
* **Versioned specs as documentation.** JSON schemas can be generated
  from the Go types and published as versioned reference documentation
  on the website. Each typed config gets its own schema page — users
  can look up exactly which fields are available for a given version.

**Cons:**

* **Less powerful.** No pattern matching across resources. Each resource
  must be listed explicitly. This is acceptable — we do not anticipate
  most users needing dynamic, expression-based routing. Pattern-based
  matching can be added via a new typed config when needed. Also, this could 
  easily be added through another typed config if users need it later.

### Option 2: Generic Transfer Configuration

A single YAML configuration file with CEL-based `match` expressions and
template-style `resource` overrides that declaratively describe the
target state of each resource.

```yaml
apiVersion: ocm.software/v1alpha1
kind: TransferConfiguration

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
```

**Pros:**

* **Powerful.** A single rule can match many resources via CEL
  expressions. Supports pattern-based routing, conditional logic, and
  access to the full descriptor data model.
* **Single format.** One configuration kind covers all use cases.

**Cons:**

* **High implementation complexity.** Requires a generic CEL evaluation
  pipeline with expression compilation, partial resource merge
  semantics, and a conversion matrix that maps (source access type ×
  target access type) to transformation chains. This is a significant
  amount of machinery for what is fundamentally a config-to-config
  compilation step.
* **Difficult to evolve and version.** The single `TransferConfiguration`
  kind bundles multiple concerns (`resourceRules`, `referenceRouting`,
  `transfers`). Evolving one section risks breaking others. Versioning
  the entire kind (`v1alpha1` → `v1beta1`) forces all sections to move
  in lockstep.
* **Difficult to switch away from.** Once users adopt CEL-based rules,
  the expressions become load-bearing configuration artifacts. Changing
  the expression language, variable names, or evaluation semantics is a
  breaking change that is hard to migrate away from.
* **CEL as end-user interface.** CEL is powerful but requires significant
  learning investment. Most users transferring components are not
  familiar with CEL syntax, optional types, or the `${...}` delimiter
  convention. Error messages from CEL evaluation failures are
  notoriously hard to interpret for non-experts.
* **Over-engineered for the common case.** The vast majority of transfer
  configurations will be "resource X goes to image reference Y". A
  match/template engine is disproportionate machinery for literal
  overrides.

## Implementation Phases

| Phase | Scope | Priority |
|---|---|---|
| **1** | `OCIImageReferenceOverride` — per-resource image reference control. Replaces reference name dependency. | Critical |
| **2** | Reference routing typed config — target overrides for recursive transfers. | Medium |
| **3** | Multi-component + multi-target transfers typed config. | Lower |

Phase 1 alone solves the most critical problem. Each phase introduces a
new typed config without modifying the existing ones.

## Open Questions

* **Wildcard / pattern matching:** Should a future typed config allow
  matching by resource type or label (e.g. "all `helmChart` resources")
  in addition to matching by name? This would reduce verbosity for
  users with many resources of the same type, without requiring full CEL.
* **Config replacements for existing flags:** Should we immediately
  introduce typed configs that supersede the current CLI flags like
  `--upload-as`? For example, a config that declares conversion
  strategies (e.g. "convert all Helm charts to OCI artifacts") would be
  more expressive than the current `--upload-as ociArtifact` flag, which
  applies uniformly and does not distinguish between source access
  types. The override configs proposed here would then layer on top to
  control *where* each converted resource ends up.
* **Field default semantics:** The current default behaviour for omitted
  `oci` fields (especially `repository` and `tag`) depends on the source
  access type. The proposed defaults are practical but should be
  revisited once we have real usage feedback — particularly whether
  falling back to the resource version for `tag` on local blobs is the
  right choice, whether erroring on a missing `repository` for local
  blobs is too strict, and whether the Helm-specific defaults (chart
  version for `tag`, required `repository`) hold up in practice. The
  Helm defaults deserve special scrutiny before final implementation
  because the chart version may diverge from the resource version, and
  it is not yet clear whether requiring `repository` or providing a
  convention-based fallback (e.g. derived from the chart name) is the
  better UX.
