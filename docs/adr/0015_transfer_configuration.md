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
* Covers the two most common transfer customisation needs (OCI image
  relocation, Helm-to-OCI conversion) without requiring users to learn
  CEL or understand the transformation engine.
* Each config type is small and independently versionable. If a type
  turns out to be wrong, it can be deprecated without affecting the
  others.
* Implementation complexity is low — each config type is a thin compiler
  to the existing transformation specification.
* The generic approach (Option 2) can still be introduced later as an
  additional, advanced option without invalidating the typed configs.

## Option 1: Typed Transfer Configurations

### Description

Instead of a single, generic configuration format, we introduce
**dedicated typed configuration resources** — one per use case. Each
type has a narrow functional scope, is independently versioned, and
compiles to transformation specification primitives.

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

The positional source and target arguments remain required
(multi-component and multi-target transfers are out of scope for
phase 1).

### Single-File Configuration

All typed configs are bundled in a single file using the existing
[generic configuration](../../bindings/go/configuration/generic/v1/spec/config.go)
wrapper (`generic.config.ocm.software/v1`). This is the same pattern
used for OCM resolver and HTTP client configuration:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: ociImageOverwrite.transfer.config.ocm.software/v1alpha1
    overwrites:
      - resource:
          name: my-pod
        imageReference: ghcr.io/target-org/images/my-pod:1.0.0

  - type: helmToOCIConversionOverwrite.transfer.config.ocm.software/v1alpha1
    overwrites:
      - resource:
          name: mariadb
        imageReference: ghcr.io/target-org/charts/mariadb:12.2.7
```

At load time, the generic config is parsed via the existing
`configuration` package. Each entry is deserialized into its concrete
Go type via `runtime.Scheme`. The transfer command collects all
recognized transfer config types from the generic wrapper and feeds
them to the compiler.

### OCIImageOverwriteConfig

Allows users to declare literal target image references for specific
resources. This directly addresses [Limitation 1](#limitation-1-no-control-over-resource-upload-locations).

```yaml
type: ociImageOverwrite.transfer.config.ocm.software/v1alpha1
overwrites:
  # Resource in the root component — no referencePath needed
  - resource:
      name: my-pod
    imageReference: ghcr.io/target-org/images/my-pod:1.0.0

  # Resource in a referenced component, disambiguated by referencePath
  - referencePath:
      - name: db-stack
    resource:
      name: monitoring-agent
      extraIdentity:
        platform: linux/amd64
    imageReference: ghcr.io/target-org/images/monitoring:2.3.1
```

#### Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `overwrites[].referencePath` | `[]ResourceIdentity` | no | Path of component reference identities from the root component to the component that owns the resource. Empty or omitted for resources in the root component. |
| `overwrites[].resource.name` | `string` | yes | Resource name |
| `overwrites[].resource.extraIdentity` | `map[string]string` | no | Extra identity attributes (for disambiguation when multiple resources share a name) |
| `overwrites[].imageReference` | `string` | yes | Literal OCI image reference for the target |

A resource identity (`name` + `extraIdentity`) is only unique within a
single component descriptor. During a recursive transfer, the same
component may appear at multiple positions in the component tree (even
in different versions). The `referencePath` traces the path of
component references from the root to the owning component, making each
entry globally unambiguous. This follows the same addressing scheme as
the controller's
[`ResourceReference`](../../kubernetes/controller/api/v1alpha1/common_types.go)
type. For resources in the root component, `referencePath` is omitted.
Each entry addresses exactly one resource — there is no pattern
matching or globbing.

#### Semantics

* Each entry identifies a resource by its reference path and resource
  identity, and declares the exact OCI image reference it should be
  uploaded to in the target.
* The compiler maps each overwrite to the appropriate Get → Add
  transformation chain, embedding the literal `imageReference` in the
  AddOCIArtifact transformation spec.
* Resources not matched by any overwrite entry fall through to the
  default behaviour (as determined by `--upload-as` / `--copy-resources`
  flags).
* Duplicate entries (same reference path + resource identity appearing
  more than once) are rejected at parse time.

#### Go Types

```go
// OCIImageOverwriteConfig declares literal target image references for
// specific resources.
type OCIImageOverwriteConfig struct {
    runtime.Type `json:",inline"`

    Overwrites []OCIImageOverwrite `json:"overwrites"`
}

type OCIImageOverwrite struct {
    ReferencePath  []ResourceIdentity `json:"referencePath,omitempty"`
    Resource       ResourceIdentity   `json:"resource"`
    ImageReference string             `json:"imageReference"`
}

// ResourceIdentity identifies a single element (resource or component
// reference) within a component descriptor. Name is always required.
// ExtraIdentity is only needed when multiple elements share the same
// name.
type ResourceIdentity struct {
    Name          string            `json:"name"`
    ExtraIdentity map[string]string `json:"extraIdentity,omitempty"`
}
```

### HelmToOCIConversionOverwriteConfig

Allows users to declare that specific Helm chart resources should be
converted to OCI artifacts and uploaded to a given image reference.
This config controls the *target location* of the conversion. A
general "convert all Helm charts to OCI" behaviour could later be
provided via a CLI flag or a separate config type — this config then
overwrites the default target location for specific charts.

```yaml
type: helmToOCIConversionOverwrite.transfer.config.ocm.software/v1alpha1
overwrites:
  - resource:
      name: mariadb
    imageReference: ghcr.io/target-org/charts/mariadb:12.2.7

  - referencePath:
      - name: db-stack
    resource:
      name: redis
    imageReference: ghcr.io/target-org/charts/redis:7.0.0
```

#### Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `overwrites[].referencePath` | `[]ResourceIdentity` | no | Path of component reference identities from the root to the owning component. Omitted for resources in the root component. |
| `overwrites[].resource.name` | `string` | yes | Resource name |
| `overwrites[].resource.extraIdentity` | `map[string]string` | no | Extra identity attributes (for disambiguation) |
| `overwrites[].imageReference` | `string` | yes | Literal OCI image reference for the converted artifact |

As with `OCIImageOverwriteConfig`, each entry is scoped via
`referencePath` and addresses exactly one resource by its identity.

#### Semantics

* Each entry identifies a Helm chart resource by its reference path
  and resource identity, and declares the OCI image reference it should
  be converted to and uploaded at.
* The compiler maps each conversion to a GetHelmChart →
  ConvertHelmToOCI → AddOCIArtifact transformation chain, embedding the
  literal `imageReference`.
* If a matched resource does not have a Helm access type, the compiler
  rejects the configuration with a clear error.
* Resources not matched by any overwrite entry are unaffected.

#### Go Types

```go
// HelmToOCIConversionOverwriteConfig declares Helm chart resources
// that should be converted to OCI artifacts at specific image
// references.
type HelmToOCIConversionOverwriteConfig struct {
    runtime.Type `json:",inline"`

    Overwrites []HelmToOCIConversionOverwrite `json:"overwrites"`
}

type HelmToOCIConversionOverwrite struct {
    ReferencePath  []ResourceIdentity `json:"referencePath,omitempty"`
    Resource       ResourceIdentity   `json:"resource"`
    ImageReference string             `json:"imageReference"`
}
```

### Alternative: Global Conversion Flag Instead of HelmToOCIConversionOverwriteConfig

`HelmToOCIConversionOverwriteConfig` bundles two concerns in one entry:
*what* to do (convert Helm to OCI) and *where* to put it (the target
image reference). These could be separated:

1. A **global conversion policy** — a CLI flag like
   `--convert-helm-to-oci` or a dedicated config type — enables
   Helm-to-OCI conversion for all Helm chart resources. The target
   location is derived automatically (same as today's `--upload-as
   ociArtifact` behaviour).
2. `OCIImageOverwriteConfig` then overrides the target image reference
   for specific resources — regardless of whether the source was
   originally OCI or Helm. Once a Helm chart is converted to an OCI
   artifact, it is indistinguishable from any other OCI resource from
   the overwrite config's perspective.

In this model:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: ociImageOverwrite.transfer.config.ocm.software/v1alpha1
    overwrites:
      # Works for originally-OCI resources
      - resource:
            name: my-pod
        imageReference: ghcr.io/target-org/images/my-pod:1.0.0

      # Also works for Helm charts converted to OCI via the global flag
      - resource:
            name: mariadb
        imageReference: ghcr.io/target-org/charts/mariadb:12.2.7
```

```bash
ocm transfer cv --convert-helm-to-oci --config transfer.yaml \
    ghcr.io/src//comp:1.0.0 ghcr.io/dst
```

This would reduce the initial typed config surface to
`OCIImageOverwriteConfig` alone. `HelmToOCIConversionOverwriteConfig`
could be dropped entirely, or deferred until a use case emerges that
genuinely requires per-resource conversion *opt-in* rather than a global
policy.

A potential downside is that this may not be intuitive: the user writes
an `OCIImageOverwrite` entry for a resource that is a Helm chart in the
source descriptor, relying on the implicit knowledge that the global
flag will have converted it to an OCI artifact by the time the overwrite
applies. The indirection between "this is a Helm chart" and "I'm
overwriting its OCI image reference" could be confusing, especially for
users unfamiliar with the conversion pipeline.

### Interaction with Existing CLI Flags

| Scenario | Behaviour |
|---|---|
| No `--config` | Today's behaviour. Positional args + flags. |
| `--config transfer.yaml` | Config entries override matching resources. Non-matching resources follow flag behaviour. |
| `--config` + `--dry-run` | Config-based generation, output the transformation spec. |

### Compilation to Transformation Specification

The typed configs are loaded once and indexed by resource identity
before graph construction begins. During `processResource` — the
existing per-resource loop inside `BuildGraphDefinition` — each
resource is checked against the index. If a matching overwrite entry
exists, its literal `imageReference` is used to emit the appropriate
transformation chain. Otherwise, the resource falls through to the
default behaviour.

```text
generic.config.ocm.software/v1 (YAML)
    │ parse via configuration package
    ▼
[]runtime.Raw
    │ deserialize each entry by type
    ▼
OCIImageOverwriteConfig           HelmToOCIConversionOverwriteConfig
    │ index by (referencePath +         │ index by (referencePath +
    │   resource identity)              │   resource identity)
    ▼                                   ▼
         transfer.BuildGraphDefinition(...)
              │
              └─ processResource (for each resource):
                   │ look up resource in overwrite index
                   │
                   ├─ match found → emit transformation chain
                   │   with literal imageReference from config
                   │
                   └─ no match → default behaviour
                        (--upload-as / --copy-resources)
```

### Adding New Typed Configurations

When a new use case arises (e.g. Maven artifact routing, reference
routing for recursive transfers), a new typed configuration is
introduced:

1. Define a new type (e.g. `MavenArtifactUploadConfig`,
   `ReferenceRoutingConfig`).
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
  needs OCI image relocation reads only `OCIImageOverwriteConfig` — no
  CEL, no match expressions, no template syntax.
* **Easy to version.** Each type evolves on its own lifecycle. A
  breaking change to `HelmToOCIConversionOverwriteConfig` does not
  affect `OCIImageOverwriteConfig`.
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

**Cons:**

* **Less powerful.** No pattern matching across resources. Each resource
  must be listed explicitly. This is acceptable — we do not anticipate
  most users needing dynamic, expression-based routing.
* **More config types over time.** Each new use case requires a new
  type. This is mitigated by the narrow scope of each type — they are
  small and quick to implement.
* **No cross-resource logic.** Cannot express "all Helm charts go to
  this registry" in a single rule. Users must list each chart
  individually. A future typed config (e.g.
  `OCIImageOverwriteByTypeConfig`) could add type-level matching without
  retroactively complicating the existing types.

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
* **Hard to validate statically.** CEL expressions are only fully
  evaluable at runtime. Typos in variable names (`resoruce.name`) or
  type mismatches (`access.imageReference` on a non-OCI resource)
  surface as runtime errors, not parse-time errors.
* **Over-engineered for the common case.** The vast majority of transfer
  configurations will be "resource X goes to image reference Y". A
  match/template engine is disproportionate machinery for literal
  overwrites.

## Implementation Phases

| Phase | Scope | Priority |
|---|---|---|
| **1** | `OCIImageOverwriteConfig` — literal per-resource OCI image reference control. Replaces reference name dependency. | Critical |
| **2** | `HelmToOCIConversionOverwriteConfig` — literal per-resource Helm-to-OCI conversion with target image reference. | High |
| **3** | Reference routing typed config — target overrides for recursive transfers. | Medium |
| **4** | Multi-component + multi-target transfers typed config. | Lower |

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
  types. The overwrite configs proposed here would then layer on top to
  control *where* each converted resource ends up.
