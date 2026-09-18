# ADR 0030: Configurable Selection of Non-SemVer Component Versions

* **Status**: proposed
* **Deciders**: Fabian Burth (@fabianburth)
* **Date**: 2026-09-17

Technical Story: Enable OCM consumers to select component versions that use SemVer or organization-specific versioning schemes without coupling repository discovery to one ordering model.

## Context and Problem Statement

OCM currently assumes relaxed Semantic Versioning for component versions. Teams also use other schemes, for example:

```text
rel-2026.T08b.01
```

This version is meaningful and lexically sortable within its domain, but it is rejected or discarded at several layers:

* the Component Descriptor schema uses `relaxedSemver` for version-bearing identities;
* `bindings/go/oci/compref/compref.go` validates component references with `VersionRegex`;
* `bindings/go/oci/internal/lister/lister.go` uses `SortPolicyLooseSemverDescending` and filters out every non-SemVer candidate;
* `bindings/go/cli/internal/repository/ocm/` hard-codes SemVer filtering and ordering;
* `bindings/go/kubernetes/controller/internal/ocm/ocm.go` and `ComponentSpec.Semver` hard-code SemVer selection and downgrade comparison;
* `bindings/go/oci/repository.go` currently distinguishes concrete versions from aliases by testing whether the tag is SemVer.

Version identity, repository representation, and version selection are separate concerns:

1. A canonical OCM version identifies a component version.
2. A repository maps that identity to its storage representation. For example, OCI currently maps SemVer `+` to `.build-` in `bindings/go/oci/semver_tag.go`.
3. A consumer may filter candidates and choose the preferred version according to a policy.

The first two concerns must support non-SemVer versions regardless of the selection mechanism. This ADR decides only how users configure filtering and preferred-version selection.

This ADR defines a new configuration API. Existing SemVer call sites show where the selected policy must eventually be integrated, but they do not determine the shape of that API.

## Decision Drivers

* Support organization-specific version schemes without auto-detecting them.
* Preserve the canonical OCM version; a filter or ordering key must not replace the selected identity.
* Make SemVer straightforward without requiring every user to write an expression.
* Use identical selection semantics in the Go library, CLI, and Kubernetes controller.
* Keep repository listing independent of ordering policy.
* Make selection deterministic and reject invalid or ambiguous results.
* Keep common policies concise and statically understandable.
* Avoid an expanding union of built-in version policy types.
* Bound evaluation cost for user-controlled policies.
* Reuse CEL, which is already a direct dependency and is used by the controller.

## Common Prerequisites

All options require the following changes outside the policy implementation:

1. Update the OCM specification and Component Descriptor schemas to define a portable, non-empty version identifier instead of requiring relaxed SemVer.
2. Generalize `bindings/go/oci/compref/compref.go` so non-SemVer versions are accepted.
3. Change `repository.ComponentVersionRepository.ListComponentVersions` to return every canonical version. Remove SemVer filtering from `bindings/go/oci/internal/lister/lister.go`; consumers own filtering and ordering.
4. Preserve canonical versions across OCI and CTF storage. Keep the existing `+` to `.build-` mapping readable and detect tag-mapping collisions.
5. Replace SemVer-based alias classification in `bindings/go/oci/repository.go`. A tag is a concrete version when it corresponds to the canonical version recorded by the referenced component descriptor; otherwise it may be an alias.
6. Add one reusable selection abstraction under `bindings/go/` and use it from the CLI and Kubernetes controller rather than implementing policy logic in each consumer.

## Considered Options

### Option 1: Flux-Style Regex Filter with Fixed Policies

#### High-Level Design

Follow the Flux ImagePolicy shape. A regular expression optionally filters or extracts values from canonical component versions. One built-in policy then orders the resulting values.

```yaml
type: versioning.config.ocm.software/v1alpha1
filterVersions:
  pattern: '^rel-[0-9]{4}\.T[0-9]{2}[a-z]\.[0-9]{2}$'
  extract: '$0'
policy:
  alphabetical:
    order: asc
```

The initial policy union contains:

```yaml
policy:
  semver:
    range: '>=1.0.0'
```

```yaml
policy:
  alphabetical:
    order: asc
```

```yaml
policy:
  numerical:
    order: asc
```

Filtering and extraction operate on canonical OCM versions. The policy compares the extracted value but returns the corresponding original version.

#### Structural / API Changes

* Add `bindings/go/configuration/versioning/v1alpha1/spec` with `FilterVersions`, `Policy`, `SemVerPolicy`, `AlphabeticalPolicy`, and `NumericalPolicy` types.
* Register the type in `bindings/go/configuration/scheme.go` and the controller configuration allow-list.
* Add a reusable selector that compiles the regex and selected policy once.
* Define deterministic tie behavior when two canonical versions produce equal extracted values.
* Integrate the selector into the CLI, controller version resolution, and downgrade checks.

#### Pros and Cons

* **Pros**:
  * Familiar Flux-compatible configuration.
  * Small and easy to explain for common cases.
  * Strong schema and admission validation.
  * Predictable runtime cost and straightforward deterministic ordering.
  * SemVer, alphabetical, and numerical behavior are explicit in the configuration.
* **Cons**:
  * Regex extraction plus fixed ordering modes forms a small custom expression language.
  * New schemes may require another built-in policy and API version.
  * Composite ordering keys are awkward. A scheme with independently ordered year, train, qualifier, and patch fields must encode them into one sortable string or number.
  * Filtering is unnecessary for repositories containing only canonical versions from one scheme.
  * The schema exposes concepts inherited from OCI tag scanning even though OCM already validates component-version artifacts while listing.

### Option 2: CEL-Only Version Selection

#### High-Level Design

Expose one CEL expression for every versioning scheme. Bind all canonical candidates as `versions: list<string>`. The expression returns the preferred canonical version or an empty string when no candidate matches.

The SAP scheme requires only lexical selection because its fields are padded:

```yaml
type: versioning.config.ocm.software/v1alpha1
policy:
  expression: 'versions.sort().last().orValue("")'
```

Filtering uses standard CEL list and regex support:

```yaml
type: versioning.config.ocm.software/v1alpha1
policy:
  expression: |
    cel.bind(
      candidates,
      versions.filter(v,
        v.matches(r'^rel-[0-9]{4}\.T[0-9]{2}[a-z]\.[0-9]{2}$')
      ),
      candidates.sort().last().orValue('')
    )
```

SemVer is also expressed through CEL. Since CEL has no standard SemVer type or precedence rules, OCM must provide a CEL function such as:

```yaml
type: versioning.config.ocm.software/v1alpha1
policy:
  expression: 'semver.latest(versions, ">=1.0.0").orValue("")'
```

The complete configuration surface is CEL; SemVer is a function in the CEL environment rather than a separate policy type.

#### Structural / API Changes

* Add `bindings/go/configuration/versioning/v1alpha1/spec` with one required CEL expression.
* Factor a generic CEL environment out of `bindings/go/kubernetes/controller/internal/cel/base.go` into a package usable by the library, CLI, and controller.
* Bind `versions` as `list<string>` and enable `ext.Lists()`, `ext.Strings()`, `ext.Math()`, `ext.Bindings()`, and `cel.OptionalTypes()`.
* Add OCM-specific CEL functions for SemVer range evaluation and preferred-version selection.
* Compile the expression once with expected result type `string`.
* Sort and deduplicate input versions lexically before evaluation.
* Validate that a non-empty result exactly equals one input version.
* Apply `cel.CostLimit` and context-aware evaluation.
* Use the same expression for normal selection and pairwise downgrade checks.

#### Pros and Cons

* **Pros**:
  * One configuration mechanism for all versioning schemes.
  * Filtering, normalization, and selection compose without additional schema fields.
  * New schemes do not require Go types, CRD fields, or configuration-version changes.
  * The SAP case remains concise.
  * CEL list, string, math, and regex functionality already exists in the current dependency.
* **Cons**:
  * Every policy, including SemVer, requires CEL syntax.
  * OCM must design and maintain custom SemVer CEL functions.
  * Schema validation can only verify the expression field; compilation and result validation happen in the consumer.
  * Runtime diagnostics and cost limits become part of every version-policy execution path.
  * An arbitrary selector is not guaranteed to define a transitive total order. Downgrade semantics rely on consistent behavior when evaluated against subsets.
  * CEL is less discoverable for users who only need a standard SemVer range.

### Option 3: First-Class SemVer with CEL Extension

#### High-Level Design

Provide a policy union with two choices:

* a first-class SemVer policy for the standard case;
* a CEL selector for arbitrary versioning schemes.

```yaml
type: versioning.config.ocm.software/v1alpha1
policy:
  semver:
    range: '>=1.0.0'
```

```yaml
type: versioning.config.ocm.software/v1alpha1
policy:
  cel:
    expression: 'versions.sort().last().orValue("")'
```

The CEL policy has the same input and output contract as Option 2. Standard CEL provides `list.filter` and `string.matches`; `cel-go` uses RE2-compatible regular expressions. Capture-group extraction can be added by enabling `ext.Regex()` when required.

SemVer selection remains ordinary Go code using `github.com/Masterminds/semver/v3`. It does not need to be exposed as a custom CEL function.

#### Structural / API Changes

* Add `bindings/go/configuration/versioning/v1alpha1/spec` with a policy union containing exactly one of `semver` or `cel`.
* Implement both policies behind one reusable selector interface.
* Reuse the existing SemVer library for `semver.range`.
* Create a restricted shared CEL environment with `versions: list<string>`, `ext.Lists()`, `ext.Strings()`, `ext.Math()`, `ext.Bindings()`, and `cel.OptionalTypes()`.
* Compile CEL expressions once with expected result type `string`.
* Sort and deduplicate CEL input versions lexically before evaluation.
* Validate that a non-empty CEL result exactly equals one input version.
* Treat an empty result as “no matching version.”
* Apply `cel.CostLimit` and context-aware evaluation.
* Use the selected policy for both normal selection and downgrade comparison.

#### Pros and Cons

* **Pros**:
  * SemVer remains concise, discoverable, and strongly schema-validated.
  * Arbitrary schemes get one general extension mechanism rather than a growing fixed-policy union.
  * No custom SemVer CEL API is required.
  * The SAP case is one short CEL expression.
  * CEL complexity and runtime cost apply only when users select CEL.
  * Both implementations share one consumer-facing selector contract.
* **Cons**:
  * Two policy implementations must be maintained and tested.
  * The policy union is larger than the CEL-only configuration.
  * Some behavior can be expressed in both branches if SemVer helper functions are later exposed to CEL.
  * CEL policies retain weaker static validation and the total-order limitation described in Option 2.

## Decision Outcome

Chosen Option: **[Option 3: First-Class SemVer with CEL Extension](#option-3-first-class-semver-with-cel-extension)**

### Justification

A fixed Flux-style policy set is easy to use but does not solve the general extensibility problem. A CEL-only API solves that problem, but requiring CEL for SemVer adds configuration and custom-function complexity to the most standardized versioning scheme.

The combined design keeps SemVer explicit because its syntax and precedence are standardized and already implemented by the current dependency. CEL handles conventions for which OCM cannot reasonably provide built-in parsers and ordering rules.

The CEL expression operates on canonical versions returned by the repository, not OCI tags. Repository storage and tag encoding therefore remain independent of selection policy.

### Consequences / Trade-offs

* `versioning.config.ocm.software/v1alpha1` initially exposes `semver` and `cel` policies; it does not expose separate `filterVersions`, `alphabetical`, or `numerical` fields.
* Exactly one policy must be configured.
* A CEL result is accepted only when it is empty or exactly one of the supplied canonical versions.
* CEL input is normalized to a deterministic lexically sorted, duplicate-free list.
* Policy compilation errors, evaluation errors, cost-limit overruns, and invalid results are configuration errors. Controllers should report them as terminal until configuration changes.
* Documentation must provide copyable recipes for lexical, reverse lexical, numerical-key, regex-filtered, and calendar-version selection.
* The shared CEL environment must remain restricted and side-effect free. Controller-specific functions such as `toOCI` are not part of version selection.
* `ext.Regex()` may be added later for extraction. Standard `matches()` is sufficient for filtering from the start.
* The inability to prove that an arbitrary CEL selector defines a total order is accepted. Tests and documentation must state the consistency requirement, especially for downgrade protection.
* The common prerequisites remain separate implementation work: accepting non-SemVer versions, listing them without SemVer filtering, preserving them through OCI mapping, and distinguishing them from aliases.
