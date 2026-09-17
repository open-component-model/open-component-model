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

The initial requirement is to select versions such as `rel-2026.T08b.01`. The design should also avoid introducing a new configuration schema whenever another versioning convention appears.

## Decision Drivers

* Support organization-specific version schemes without auto-detecting them.
* Preserve the canonical OCM version; a filter or ordering key must not replace the selected identity.
* Preserve current SemVer behavior by default.
* Use identical selection semantics in the Go library, CLI, and Kubernetes controller.
* Keep repository listing independent of ordering policy.
* Make selection deterministic and reject invalid or ambiguous results.
* Keep the common case concise.
* Avoid an expanding union of built-in version policy types.
* Bound evaluation cost for user-controlled policies.
* Reuse CEL, which is already a direct dependency and is used by the controller.

## Common Prerequisites

Both options require the following changes outside the policy implementation:

1. Update the OCM specification and Component Descriptor schemas to define a portable, non-empty version identifier instead of requiring relaxed SemVer.
2. Generalize `bindings/go/oci/compref/compref.go` so non-SemVer versions are accepted without a compatibility escape hatch.
3. Change `repository.ComponentVersionRepository.ListComponentVersions` to return every canonical version. Remove SemVer filtering from `bindings/go/oci/internal/lister/lister.go`; consumers own filtering and ordering.
4. Preserve canonical versions across OCI and CTF storage. Keep compatibility with the existing `+` to `.build-` mapping and detect tag-mapping collisions.
5. Replace SemVer-based alias classification in `bindings/go/oci/repository.go`. A tag is a concrete version when it corresponds to the canonical version recorded by the referenced component descriptor; otherwise it may be an alias.
6. Retain existing SemVer fields and flags as compatibility shorthands during migration.

## Considered Options

### Option 1: Regex Filter with Fixed Selection Policies

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

Filtering and extraction operate on canonical OCM versions. The policy compares the extracted value, but returns the corresponding original version.

#### Structural / API Changes

* Add `bindings/go/configuration/versioning/v1alpha1/spec` with `FilterVersions`, `Policy`, `SemVerPolicy`, `AlphabeticalPolicy`, and `NumericalPolicy` types.
* Register the type in `bindings/go/configuration/scheme.go` and the controller configuration allow-list.
* Add a reusable selector package under `bindings/go/` that compiles the regex and selected policy once.
* Replace the CLI and controller SemVer implementations with this selector.
* Evolve `ComponentSpec.Semver` and `ComponentSpec.SemverFilter` toward the shared configuration shape while retaining compatibility handling.
* Define deterministic tie behavior when two canonical versions produce equal extracted values.

#### Pros and Cons

* **Pros**:
  * Familiar Flux-compatible configuration.
  * Small and easy to explain for common cases.
  * Strong schema and admission validation.
  * Predictable runtime cost and straightforward deterministic ordering.
  * SemVer behavior maps directly to the existing implementation.
* **Cons**:
  * Regex extraction plus three ordering modes is a small custom expression language.
  * New schemes may require another built-in policy and API version.
  * Composite ordering keys are awkward. A scheme with independently ordered year, train, qualifier, and patch fields must encode those fields into one sortable string or number.
  * Filtering is unnecessary for repositories containing only canonical versions from one scheme.
  * The schema exposes concepts inherited from OCI tag scanning even though OCM already validates component-version artifacts while listing.

### Option 2: CEL Version Selector

#### High-Level Design

Bind all canonical candidates as `versions: list<string>` and evaluate one CEL expression. The expression returns the preferred canonical version or an empty string when no candidate matches.

The SAP scheme requires only lexical selection because its fields are padded:

```yaml
type: versioning.config.ocm.software/v1alpha1
policy:
  cel:
    expression: 'versions.sort().last().orValue("")'
```

Filtering uses standard CEL list and regex support; no OCM-specific regex filter is needed:

```yaml
type: versioning.config.ocm.software/v1alpha1
policy:
  cel:
    expression: |
      cel.bind(
        candidates,
        versions.filter(v,
          v.matches(r'^rel-[0-9]{4}\.T[0-9]{2}[a-z]\.[0-9]{2}$')
        ),
        candidates.sort().last().orValue('')
      )
```

Custom ordering can derive a key with CEL's `sortBy`:

```yaml
type: versioning.config.ocm.software/v1alpha1
policy:
  cel:
    expression: |
      versions
        .sortBy(v, int(v.split('.')[2]))
        .last()
        .orValue('')
```

Standard CEL already provides `list.filter` and `string.matches`; `cel-go` uses RE2-compatible regular expressions. The existing controller environment in `bindings/go/kubernetes/controller/internal/cel/base.go` already enables `ext.Lists()`, `ext.Strings()`, `ext.Math()`, `ext.Bindings()`, and `cel.OptionalTypes()`.

Capture-group extraction is available through `cel-go`'s `ext.Regex()` (`regex.extract`, `regex.extractAll`, and `regex.replace`). It is not currently enabled in the controller environment and is not required for the initial SAP example.

SemVer remains a first-class compatibility policy:

```yaml
type: versioning.config.ocm.software/v1alpha1
policy:
  semver:
    range: '>=1.0.0'
```

This avoids reimplementing SemVer precedence in CEL and provides a direct migration for existing fields and flags. CEL is the extensibility option for non-SemVer schemes.

#### Structural / API Changes

* Add `bindings/go/configuration/versioning/v1alpha1/spec` with a policy union containing `semver` and `cel`.
* Add a reusable version selector package under `bindings/go/`; do not place selection in the controller or OCI repository.
* Move or factor the generic CEL environment from `bindings/go/kubernetes/controller/internal/cel/base.go` into a shared package such as `bindings/go/cel` or `bindings/go/version` so the CLI and controller compile the same expressions.
* Build a restricted environment with `versions: list<string>`, `ext.Lists()`, `ext.Strings()`, `ext.Math()`, `ext.Bindings()`, and `cel.OptionalTypes()`. Add `ext.Regex()` only if capture extraction becomes a requirement.
* Compile expressions once with expected result type `string`.
* Sort and deduplicate input versions lexically before evaluation so repository enumeration order cannot affect the result accidentally.
* Validate that a non-empty result exactly equals one input version. CEL must not synthesize a new canonical version.
* Treat an empty result as “no matching version.”
* Apply `cel.CostLimit` and context-aware evaluation.
* For downgrade checks, evaluate the same selector against the current and candidate versions. A candidate omitted by the policy is invalid; a candidate selected over the current version is not a downgrade.
* Keep current SemVer code behind the `semver` policy and map existing `--semver-constraint`, `ComponentSpec.Semver`, and `ComponentSpec.SemverFilter` behavior during migration.

#### Pros and Cons

* **Pros**:
  * One extensibility mechanism covers filtering, parsing, normalization, and selection.
  * New version schemes do not require Go types, CRD fields, or configuration-version changes.
  * The SAP case remains concise: `versions.sort().last().orValue("")`.
  * CEL list, string, math, and regex functionality already exists in the current dependency.
  * The same policy can be evaluated by the library, CLI, and controller.
  * Avoids copying Flux's fixed policy model into OCM when OCM already has CEL infrastructure.
* **Cons**:
  * CEL is less discoverable than explicit `alphabetical` or `numerical` fields.
  * Schema validation can verify the expression is a string, but compilation and type validation happen in the consumer.
  * Runtime failures need clear terminal errors containing the expression and CEL diagnostics.
  * An arbitrary selector is not guaranteed to define a transitive total order. Downgrade semantics rely on the policy behaving consistently when evaluated against subsets.
  * User-controlled comprehensions and regular expressions require cost limits and candidate-count testing.
  * The current reusable CEL environment is controller-internal and must be factored into a package usable by all consumers.

## Decision Outcome

Chosen Option: **[Option 2: CEL Version Selector](#option-2-cel-version-selector)**

### Justification

The requirement is not to support one additional known version grammar; it is to stop coupling OCM to one versioning scheme. A fixed regex-plus-policy union removes the SemVer restriction but replaces it with another closed set of ordering strategies.

CEL is already used by the controller and the current `cel-go` dependency provides the required list sorting, key-based sorting, string processing, and regex matching. The initial SAP policy is shorter than the equivalent structured configuration and requires no custom parser. Future schemes can derive keys or filter candidates without changing the OCM configuration API.

SemVer remains explicit because it has standardized precedence rules, existing compatibility fields, and an established implementation. The resulting policy union has one stable built-in and one general extension point rather than a growing set of special-purpose policies.

The CEL expression operates on canonical versions returned by the repository, not OCI tags. Repository storage and tag encoding therefore remain independent of selection policy.

### Consequences / Trade-offs

* `versioning.config.ocm.software/v1alpha1` initially exposes `semver` and `cel` policies; it does not expose separate `filterVersions`, `alphabetical`, or `numerical` fields.
* Existing behavior remains SemVer when no versioning configuration is supplied.
* A CEL result is accepted only when it is empty or exactly one of the supplied canonical versions.
* CEL input is normalized to a deterministic lexically sorted, duplicate-free list.
* Policy compilation errors, evaluation errors, cost-limit overruns, and invalid results are configuration errors. Controllers should report them as terminal until configuration changes.
* Documentation must provide copyable recipes for lexical, reverse lexical, numerical-key, regex-filtered, and calendar-version selection.
* The shared environment must remain restricted and side-effect free. Controller-specific functions such as `toOCI` are not part of version selection.
* `ext.Regex()` may be added later for extraction. Standard `matches()` is sufficient for filtering from the start.
* The inability to prove that an arbitrary CEL selector defines a total order is accepted. Tests and documentation must state the consistency requirement, especially for downgrade protection.
* The common prerequisites remain separate implementation work: accepting opaque versions, listing them without SemVer filtering, preserving them through OCI mapping, and distinguishing them from aliases.
