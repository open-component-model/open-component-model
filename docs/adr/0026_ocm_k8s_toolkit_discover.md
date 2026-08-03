# Component Graph Discovery in the OCM Kubernetes Controller Toolkit

* **Status**: proposed
* **Deciders**: OCM Maintainer Team
* **Date**: 2026-07-28

Technical Story:

- EPIC: [ocm-project#1153](https://github.com/open-component-model/ocm-project/issues/1153)
- Spike: [ocm-project#1154](https://github.com/open-component-model/ocm-project/issues/1154)
- POC branch: [`feat-discovery`](https://github.com/frewilhelm/open-component-model/tree/feat-discovery)
- Related upstream work: [PR #2833 (`feat(oci): blob and resolution cache`)](https://github.com/open-component-model/open-component-model/pull/2833),
  [ocm-project#296 (`ResourceID.BySelector`)](https://github.com/open-component-model/ocm-project/issues/296)

## Context and Problem Statement

The OCM Kubernetes Controller Toolkit currently exposes an identity-based consumption model: a user creates a
`Repository`, then a `Component` pinned to a single semver constraint, then one `Resource` per artifact they want to
consume. Every level of that chain requires the caller to know the exact identity of what they are pulling:
repository spec, component name, resource identity, and (for nested references) the full `referencePath`.

Two external consumer projects asked for a query-based mode on top of that identity-based baseline:

- OpenControlPlane (OCP): The [openmcp-project](https://github.com/openmcp-project) org, needs a single custom
  resource to span umbrella components with many nested references, match resources by selector, and project the
  matched artifacts into a form its own controllers can consume. Requirements are tracked in
  [ocm-project#1153](https://github.com/open-component-model/ocm-project/issues/1153).
- Open Delivery Gear (ODG): The [Open Delivery Gear](https://github.com/open-component-model/odg-core) project, needs to
  resolve component descriptors on demand, enumerate the versions available for a component, filter by publication date,
  and download resources for external scanners (SBOM, vulnerability tooling). ODG's pipelines are synchronous and the
  CR-reconcile round-trip is a blocker. That is why ODGs requirements are out of scope for this ADR.
  Details in [ocm-project#1154](https://github.com/open-component-model/ocm-project/issues/1154).

Neither workflow is expressible today. A caller who does not know the exact
`(component, version, resource, referencePath)` tuple up front has no way to ask the controller
"give me all versions of resource R across umbrella C's references" or
"publish this component's descriptor so I can navigate it".

## Decision Drivers

- Familiar shapes where possible: `matchLabels`-shaped label filtering and CEL for structured predicates:
  the same shapes users see in other CRDs. OCM-specific identity attributes get their own field (`matchIdentity`).
- Kubernetes etcd payload cap: Objects have a soft limit of [~1.5 MiB](https://etcd.io/docs/v3.6/dev-guide/limit/)
  per resource; component descriptors can approach or exceed it on their own (e.g.
  `europe-docker.pkg.dev/gardener-project/releases//github.com/gardenlinux/gardenlinux:2150.6.0` is ~884 KB compact
  JSON).
- Additive to the existing identity-based CRDs: `Repository`, `Component`, and `Resource` are already consumed
  by ArgoCD/FluxCD integrations. Discovery must not break the current API contract.
- API contract first: Focus on API contract to enable OCP's use case and tackle performance later. 

## Considered Options

Extend `Resource` with a `discover` payload: Rejected on separation of concerns and shape.

Dedicated `Discovery` CRD (chosen): See [Decision Outcome](#decision-outcome). Earlier CRD sketches from the
spike (`ResourceRange`, `ResourceSet`, `ComponentDescriptor`) are on
[ocm-project#1153](https://github.com/open-component-model/ocm-project/issues/1153); each covers one query shape
and needed siblings to cover the others. The chosen CRD subsumes all three by covering all three filtering
steps in a single kind.

## Decision Outcome

Chosen option: **a dedicated `Discovery` CRD** with three selectors and a CEL projection. Targets
OpenControlPlane's shape. Descopes the large-descriptor navigation case (see [etcd size](#etcd-size)) and leaves
ODG's synchronous-access need out of scope.

### CRD shape

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Discovery
metadata:
  name: flux-images
  namespace: default
spec:
  # ... shared spec fields (ocmConfig, suspend) omitted; see other CRDs.

  # Required. Same-namespace reference to a Component CR.
  componentRef:
    name: releasechannel

  # Optional. Filters references. A component is kept if it is the target of
  # at least one matching reference somewhere below the root. The root has no
  # incoming reference and is therefore dropped when this selector carries at
  # least one predicate; an empty `{}` selector is treated the same as an
  # omitted field and preserves the full graph including the root.
  referenceSelector:
    matchIdentity:
      componentName: ghcr.io/example/releasechannel/flux
    expression: semverCheck(identity.version, ">=2.7.0, <2.10.0")

  # Optional. Filters components by their own identity and labels. Applied to
  # whatever components survived referenceSelector; when referenceSelector is
  # unset (or empty), that's the full graph including the root.
  componentSelector:
    matchLabels:
      tier: platform

  # Optional. Applied in place on each surviving descriptor's Component.Resources.
  # Nil/empty means keep all resources.
  resourceSelector:
    expression: identity.name in ["flux", "image-automation-controller"]

  # Optional. Projects the discovered content. Exactly one of byResources,
  # byComponents, or expression may be set (enforced by CRD validation).
  extract:
    byResources:
      imageRef:        resource.access.imageReference
      resourceName:    resource.name
      resourceVersion: resource.version
      name:            component.name
      version:         component.version

status:
  # ... shared status fields (observedGeneration, effectiveOCMConfig,
  # standard Ready/Reconciling/Stalled conditions) omitted; see other CRDs.

  # Schemaless. Shape depends on spec, see Status reasoning below.
  discovery:
    - imageRef: ghcr.io/fluxcd/flux:2.8.1
      resourceName: flux
      resourceVersion: 2.8.1
      name: ghcr.io/example/releasechannel/flux
      version: 2.8.1
    - imageRef: ghcr.io/fluxcd/image-automation-controller:2.8.1
      resourceName: image-automation-controller
      resourceVersion: 2.8.1
      name: ghcr.io/example/releasechannel/flux
      version: 2.8.1
```

### Spec reasoning

#### `componentRef` 

`Component` already resolves `(Repository, semver)` into a `{repositorySpec, component, version, digest}`. `Discovery`
watches that Component.

#### Three selectors (reference / component / resource)

The three selectors map 1:1 to the three kinds of things you can filter on in the OCM data model:

- `referenceSelector` filters **references** in the graph. A reference is a `component.references[]` entry: it carries
  its own identity (local `name`, target `componentName`, `version`, extra identity) and its own labels.
- `componentSelector` filters **components**: the descriptors themselves, by their own identity and labels.
- `resourceSelector` filters **resources within a surviving component**. Applied in place; empty selector keeps all.

Traversal semantics under filters: Filters are applied as post-order predicates over the full reachable graph,
not as per-edge pruning during traversal. Per-edge pruning would drop deep matches whose ancestor edges happen not
to match. This trades a broader graph walk for correctness under nested reference structures. The graph walk is
concurrent and unbounded today; a concurrency limit might be a follow-up.

Short-circuit: When a selector fixes a component identity, `name` and `version` set as concrete equality, no
other constraints active, the controller extracts that target and terminates DAG traversal at the vertex's subtree
boundary. Component identity `{name, version}` is [globally unique per the OCM
spec](https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/02-elements-toplevel.md#component-identity),
so nothing below the target needs to be walked. Label and expression constraints disable the short-circuit; the
controller can't tell what they'll evaluate to before actually running them. This is why `MatchIdentity` is a
first-class map on the CR surface (see [Selector shape](#selector-shape) below) rather than folded into `Expression`:
the controller can just read the `name` and `version` entries directly and decide whether short-circuit applies, without
compiling or evaluating CEL.

#### Selector shape

Selectors have three fields, all ANDed. An empty selector matches everything.

- `matchIdentity`: equality on identity attributes (`name`, `version`, and per-element extras like `componentName` on
  references). First-class map on the CR surface so the controller can read it statically for the short-circuit
  optimisation.
- `matchLabels`: string-equality on labels whose value is a string. OCM labels may hold arbitrary JSON; non-string
  values are not comparable this way and are silently non-matching.
- **`expression`**: a CEL boolean predicate. A CEL binding is a named value the expression can reference; for
  selectors the controller binds `identity` to the element's identity map and `labels` to its label map, so users
  write `identity.version` or `labels["tier"]`. `expression` covers structured labels, cross-field predicates, and
  semver ranges (`semverCheck(v, c)`). Missing attribute or label references evaluate to `false` rather than raising,
  so users don't have to guard every access with `has()`.

##### Considered and rejected: k8s-standard `matchExpressions`

Adding `matchExpressions []metav1.LabelSelectorRequirement`, the `In` / `NotIn` / `Exists` / `DoesNotExist`
operators from `metav1.LabelSelector`, was considered because it's a familiar k8s shape and would let admission
tooling that walks `LabelSelector`-shaped fields work on the string subset without changes. Not adopted, for three
reasons:

- Two ways to write the same query: `env in [prod, staging]` works in both `matchExpressions` and CEL.
- `LabelSelector` values are string-only: OCM labels are arbitrary JSON. `matchExpressions` covers only the
  string subset; structured labels still need `expression`. Adding it doesn't remove any surface, only adds.
- The one range operator users actually need, semver, is already in CEL as `semverCheck`.

#### `Extract` and its three modes

`Extract` is the projection stage: it runs *after* filtering, on the already-narrowed descriptor list, and produces
the final payload landing in `status.discovery`. Three mutually exclusive modes cover the three join shapes a graph
query produces, enforced by CRD-level validation:

- `byResources: {field: expr}`: flat map, evaluated once per surviving resource of each surviving component.
  Bindings: `resource`, `component`. Emits `[]object`, one entry per `(component, resource)` pair.
- `byComponents: {field: expr}`: same shape, evaluated once per surviving component. Binding: `component`.
  Emits `[]object`.
- `expression: <cel>`: single CEL expression whose return value is stored verbatim. Binding: `components`
  (list of surviving descriptors). Emits any JSON type. Use when the map modes cannot express the projection
  (cross-graph joins, custom shapes, computed fields).

In map modes (`byResources` / `byComponents`), a field expression that hits a missing attribute, field, or map
key is treated as null and the field is dropped from that entry. This lets a single map span heterogeneous access
types (e.g. some resources carry `access.imageReference`, others don't) without CEL `?` on every path, matching the
tolerance selectors have for missing attributes (see [Selector shape](#selector-shape) above). To emit a
placeholder instead of dropping, use `has(...)` with a conditional, e.g.
`has(resource.access.imageReference) ? resource.access.imageReference : "n/a"`.

`expression` mode has no per-field container to drop into: the whole payload is a single CEL expression, so an
unguarded miss surfaces as `ExtractFailed`. Guard with `has(...)` or CEL optional access
(`foo.?bar.orValue(...)`) if the expression may traverse fields that are not always present.

`Extract` is optional. When absent, the raw v2 descriptor list is emitted, subject to the etcd soft limit.

#### No `Interval`: watch-driven reconciliation

Discovery has no periodic reconcile. Re-reconciliation is driven by:

- Discovery-generation change on the CR itself.
- Change to the referenced Component's resolved version, effective OCM config, or readiness state, mapped through a
  field-indexed lookup back to every Discovery referencing that Component.

Discovery is a projection of upstream state. There is no upstream state it should notice on a timer that its watches
would not already surface. Adding an `Interval` would only paper over gaps in the event source.

### Status reasoning

#### `discovery *apiextensionsv1.JSON`: schemaless by construction

```go
// +kubebuilder:validation:XPreserveUnknownFields
// +kubebuilder:validation:Schemaless
// +optional
Discovery *apiextensionsv1.JSON `json:"discovery,omitempty"`
```

Payload shape is a function of `spec`. Enumerating the shapes:

- No selectors, no extract: JSON *array* of surviving descriptors (always an array, even when there is exactly
  one, consumers can iterate uniformly).
- `referenceSelector`: array of target descriptors (root excluded; it has no incoming reference).
- `componentSelector`: array of matched descriptors; runs after `referenceSelector` (if set), so it operates on
  whatever survived that stage.
- `resourceSelector`: array of descriptors with `.component.resources` filtered in place.
- `extract.byResources`: flat array of per-resource projections.
- `extract.byComponents`: flat array of per-component projections.
- `extract.expression`: verbatim return value of the CEL expression (any JSON type).

The same schemaless pattern is used by the Resource CR for its CEL-projection output, consumers parse both status
payloads generically with the same code. The payload always lives on `status` (not a child CR, ConfigMap, or artifact).

#### etcd size

Status payloads are subject to the etcd soft limit (~1.5 MiB). Discovery does not store descriptors outside etcd.
Selectors and `Extract` let users shape the payload down. When the result still exceeds the limit, the API server
rejects the status write; the controller clears `status.discovery`, repatches a `Ready=False` /
`PayloadTooLarge` condition carrying the API server's rejection message, and returns without requeueing.

The large-descriptor navigation case is descoped. Out-of-status storage (encoded payload,
sibling ConfigMap, artifact CR) is an additive path if a concrete need appears.

#### Deterministic ordering: `(name, version)` lexicographic

Descriptors are sorted by `(component.name, component.version)` before projection. Without this, the map iteration
order of the underlying graph store would make `status.discovery` flap on every reconcile, causing watcher churn on
downstream consumers and generating unnecessary etcd writes.

#### Conditions and reasons

Discovery uses the shared condition types and reasons, plus Discovery-specific reasons:

- `SelectorFailed`: a `spec` selector failed to compile or evaluate.
- `ExtractFailed`: a `spec.extract` projection failed to compile or evaluate.
- `NoReferencesMatched` and `NoComponentsMatched`: emitted on `Ready=True` when a selector filters the
  descriptor set to empty. In that state `status.discovery` is set to an explicit empty JSON array (`[]`) so
  consumers can distinguish "query ran and matched nothing" from "payload never computed". Distinct reasons per
  stage so consumers can distinguish "no reference matched" from "reference matched but the component didn't".

Consumers of `status.discovery` follow `status.effectiveOCMConfig` when they need credentials (pull-secrets, etc.)
for the discovered resources.

## Caching Strategy

Discovery has no cross-reconcile descriptor cache today; every reconcile refetches every reachable descriptor from
the upstream repository. The plan is to rely on the component descriptor cache landing upstream in
[PR #2833](https://github.com/open-component-model/open-component-model/pull/2833), which caches descriptor bytes
on disk across all consumers of the OCI provider. The existing resolution service is not wired in, its removal is
contingent on the upstream cache landing; if the upstream cache is delayed, the resolution service remains available
as a fallback.

## Trade-offs of the chosen CRD

Pros:

- One CRD, three filter kinds, one projection language.
- Uses CEL, already in the module.
- Schemaless status matches the intrinsically-variable output shape.
- Selector shape is reusable for `ResourceID.BySelector`.
- Watch-driven; no additional polling load.

Cons:

- Larger schema and larger conceptual surface than a specialised, single-shape CRD.
- Schemaless status loses server-side field validation on the payload; consumers must parse defensively.
- Two ways to write some queries (e.g. semver via CEL vs. equality via `matchLabels`).
- Etcd size risk on unprojected large descriptors; mitigation is documentation-plus-`Extract`, not a hard guard.

## Follow-ups

- Concurrency limit on graph traversal: `syncdag.Discover` spawns one goroutine per neighbor with no upper
  bound. The `Concurrency` option that already exists on `GraphProcessorOptions`
  ([`bindings/go/dag/sync/process_options.go`](../../bindings/go/dag/sync/process_options.go)) needs to be added
  to `GraphDiscovererOptions` (see the existing TODO in
  [`discover_options.go`](../../bindings/go/dag/sync/discover_options.go) referencing
  [ocm-project#705](https://github.com/open-component-model/ocm-project/issues/705)), then threaded through
  Discovery's caller.
- `cel.CostLimit` on `Selector.Expression` and `Extract.Expression`: Discovery has no `Interval` and no default
  per-reconcile deadline. A pathological CEL program (e.g. a regex-`matches` on user-controlled input, or a nested
  `.filter()` over a large graph) currently has no static bound on evaluation cost. `cel-go` exposes
  `cel.CostLimit(n)`, the same mechanism `x-kubernetes-validations` uses, which counts AST-cost units at
  evaluation time and aborts on overrun. Adding it makes worst-case eval time bounded and surfaces the error as
  `Ready=False` with reason `ExtractFailed` (and, when non-retriable, also `Stalled=True`) instead of runaway CPU.
  Needs benchmark input to pick sensible defaults per binding scope.
- Unify `Selector` with `ResourceID.BySelector`: Once
  [ocm-project#296](https://github.com/open-component-model/ocm-project/issues/296) lands, verify the same
  `Selector` shape serves both consumers without further extension. Any divergence should be resolved on the
  shared type.
- Multi-root `componentRef`: A single Discovery CR targets one root Component today. Users needing an n:m shape
  compose it with multiple Discovery CRs or an umbrella-of-umbrellas Component. We might think of a
  `componentRefs []ComponentRef` in the future.
- Per-descriptor digest verification: Recompute each transitively-resolved descriptor's digest and compare
  against the digest asserted by its parent reference; surface a per-entry integrity signal on `status.discovery`.
  Not shipped: recomputing digests on every reconcile is expensive on large graphs. Open question whether to enable
  this later as an opt-in flag.

## Conclusion

Discovery ships as one CRD that projects a filtered slice of a component's transitive reference graph into a
schemaless `status.discovery` payload. It targets OpenControlPlane's shape, descopes large-descriptor navigation,
and leaves ODG's synchronous-access needs out of scope. Caching is delegated to the component descriptor cache
landing upstream in PR #2833.
