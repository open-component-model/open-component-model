# Component Graph Discovery in the OCM Kubernetes Controller Toolkit

* **Status**: proposed
* **Deciders**: OCM Maintainer Team
* **Date**: 2026-07-28

Technical Story: The OCM Kubernetes Controller Toolkit only supports identity-based consumption today: every
`Repository` / `Component` / `Resource` CR must be pinned to an exact component name, version, and resource identity.
Downstream consumers (OpenControlPlane, Open Delivery Gear) need a query-based mode that discovers a component
version together with its references (if any) and projects a filtered subset of the discovered descriptors and
resources into Kubernetes-visible state. This ADR decides the API surface for that mode.

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

Neither workflow is expressible today. Currently, the purpose of the OCM K8s Toolkit is to deploy a resource to a
cluster that has a known identity. There is no way to ask the controller
"give me all versions of resource R across umbrella C's references" or
"publish this component's descriptor so I can navigate it".

Tracking issues and related work:

- EPIC: [ocm-project#1153](https://github.com/open-component-model/ocm-project/issues/1153)
- Spike: [ocm-project#1154](https://github.com/open-component-model/ocm-project/issues/1154)
- POC branch: [`feat-discovery`](https://github.com/frewilhelm/open-component-model/tree/feat-discovery)
- Related upstream work: [PR #2833 (`feat(oci): blob and resolution cache`)](https://github.com/open-component-model/open-component-model/pull/2833),
  [ocm-project#296 (`ResourceID.BySelector`)](https://github.com/open-component-model/ocm-project/issues/296)

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

Chosen option: **a dedicated `Discovery` CRD** with three selectors that filter the transitively reachable
descriptors and an optional `Extract` stage of CEL expressions that build the records in `status.extracted` from
those descriptors and their resources. Targets OpenControlPlane's shape. Descopes the large-descriptor navigation case
(see [etcd size](#etcd-size)) and leaves ODG's synchronous-access need out of scope.

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

  # Optional. What to do when a reachable reference cannot be resolved from the
  # root's repository. `Fail` (default) aborts the reconcile. `Ignore` emits the
  # components that did resolve and records the rest in `status.unresolved`.
  onResolutionError: Fail

status:
  # ... shared status fields (observedGeneration, effectiveOCMConfig,
  # standard Ready/Reconciling/Stalled conditions) omitted; see other CRDs.

  # Written only when `spec.extract` is set. Free-form records.
  extracted:
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

  # Written only when `spec.extract` is absent. Raw v2 descriptors.
  # components:
  #   - meta: {schemaVersion: v2}
  #     component:
  #       name: ghcr.io/example/releasechannel/flux
  #       version: 2.8.1
  #       resources:
  #         - name: flux
  #           version: 2.8.1
  #           type: ociArtifact
  #           access: {type: ociArtifact, imageReference: ghcr.io/fluxcd/flux:2.8.1}

  # Controller-owned. Only populated under `onResolutionError: Ignore`.
  unresolved:
    - component: ghcr.io/example/releasechannel/notification-controller
      version: 1.4.0
      reason: GetComponentVersionFailed
      message: 'not found in repository ghcr.io/example'
```

### Spec reasoning

#### `componentRef` 

`Component` already resolves `(Repository, semver)` into a `{repositorySpec, component, version, digest}`. `Discovery`
watches that Component.

The resolved `{component, version}` in that Component's `status.component` is the **root** of the discovery graph, and
its `repositorySpec` and `effectiveOCMConfig` scope the entire traversal: every transitively reachable component is
resolved from that one repository with those credentials. The descriptors' `repositoryContexts` are not consulted, so a
reference that is only resolvable elsewhere does not resolve.

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

Short-circuit: When a selector fixes a component identity `name` and `version` set as concrete equality on
`matchIdentity` the controller marks that identity as a short-circuit target. Once the DAG traversal resolves it,
the target's `Discover` call returns an early-exit signal that propagates through the discoverer's `errgroup` and
stops the walk. Component identity `{name, version}` is [globally unique per the OCM
spec](https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/02-elements-toplevel.md#component-identity),
so at most one vertex can match and nothing else needs to be walked.

The two selectors have different envelopes for the optimization:

- `componentSelector`: `matchLabels` and `expression` are allowed alongside `matchIdentity`. The
  post-filter (`filterByComponentSelector`) evaluates them against the target's own descriptor. The
  short-circuit fires from the target's own `Discover` call, which runs only after `Resolve` has already stored
  the descriptor on the vertex, so the target is guaranteed to be in the discovered set. Cancelled siblings are
  irrelevant: their descriptors have different identities, so the component-level post-filter would drop them
  anyway.

- `referenceSelector`: `matchLabels` and `expression` must be empty, and `matchIdentity` must contain
  exactly `componentName` and `version` (no other keys). Reference labels and per-edge identity attributes
  (local `name`, extra identity) live on edges, and the same target can be reached via multiple parents. The
  early-exit signal propagates through the discoverer's `errgroup` and cancels sibling goroutines, some of
  which may still be resolving alternative parents of the target. Those parents' descriptors then never enter
  `filterByReferenceSelector`, which iterates each surviving descriptor's `Component.References` to decide
  which targets match. Cancelling a parent hides its reference edge to the target from the post-filter, so a
  `matchLabels` / `expression` clause, or a `matchIdentity` clause on a per-edge attribute, that would have
  matched on the cancelled edge produces a false negative. Restricting the short-circuit trigger to a
  `matchIdentity` of exactly `{componentName, version}` with no label/expression clauses keeps it to cases
  where the post-filter is a no-op (any surviving edge to the target is a match).

This is why `matchIdentity` is a first-class map on the CR surface (see [Selector shape](#selector-shape) below)
rather than folded into `expression`: the controller reads `name` and `version` directly and decides whether
short-circuit applies without compiling or evaluating CEL.

#### Selector shape

Selectors have three fields, all ANDed. An empty selector matches everything.

- `matchIdentity`: equality on identity attributes (`name`, `version`, and per-element extras like `componentName` on
  references). First-class map on the CR surface so the controller can read it statically for the short-circuit
  optimization.
- `matchLabels`: string-equality on labels whose value is a string. OCM labels may hold arbitrary JSON; non-string
  values are not comparable this way and are silently non-matching.
- `expression`: a CEL boolean predicate. A CEL binding is a named value the expression can reference; for selectors
  the controller binds `identity` to the element's identity map and `labels` to its label map, so users write
  `identity.version` or `labels["tier"]`. `expression` covers what the map fields cannot: structured (non-string)
  label values, semver ranges via `semverCheck(v, c)`, and predicates that relate more than one field, e.g.
  `labels["tier"] == "platform" && identity.version.startsWith("2.")`. Missing attribute or label references evaluate
  to `false` rather than raising, so users don't have to guard every access with `has()`. Without this behaviour,
  mismatches would return an error instead of "dropping the filter". For example, resources can have different fields
  depending on their type. Thus, a component version with heterogeneous resources can be filtered with a single
  `expression` without having to guard every path.

Why bother with `matchIdentity` and `matchLabels` when `expression` can express the same queries? Convenience.
`matchIdentity` and `matchLabels` are familiar shapes from other CRDs, and they let users write simple queries
without CEL knowledge. As stated above, `matchIdentity` also lets the controller short-circuit the graph walk when a
concrete identity (component name + version) is requested, which is a performance win on large graphs.

##### Considered and rejected: k8s-standard `matchExpressions`

Adding `matchExpressions []metav1.LabelSelectorRequirement`, the `In` / `NotIn` / `Exists` / `DoesNotExist`
operators from `metav1.LabelSelector`, was considered because it's a familiar k8s shape and would let admission
tooling that walks `LabelSelector`-shaped fields work on the string subset without changes. 

The field was omitted as its semantics are redundant with `expression`. Additionally, `expression` is far more flexible.

#### `Extract` and its three modes

`Extract` is the output-shaping stage: it runs *after* filtering, on the already-narrowed descriptor list, and produces
the final payload landing in `status.extracted`. Three mutually exclusive modes cover the three join shapes a graph
query produces, enforced by CRD-level validation:

- `byResources: {field: expr}`: flat map, evaluated once per surviving resource of each surviving component.
  Bindings: `resource`, `component`. Emits `[]object`, one entry per `(component, resource)` pair.
- `byComponents: {field: expr}`: same shape, evaluated once per surviving component. Binding: `component`.
  Emits `[]object`.
- `expression: <cel>`: single CEL expression whose return value is stored verbatim. Binding: `components`
  (list of surviving descriptors). Emits any JSON type. Use when the map modes cannot express the desired output
  shape (cross-graph joins, custom shapes, computed fields).

In map modes (`byResources` / `byComponents`), a field expression that hits a missing attribute, field, or map
key is treated as null and the field is dropped from that entry. This lets a single map span heterogeneous access
types (e.g. some resources carry `access.imageReference`, others don't) without CEL `?` on every path, matching the
tolerance selectors have for missing attributes (see [Selector shape](#selector-shape) above). To emit a
placeholder instead of dropping, use `has(...)` with a conditional, e.g.
`has(resource.access.imageReference) ? resource.access.imageReference : "n/a"`.

`expression` mode is strict on missing-field access: any miss surfaces as `ExtractFailed`. The tolerance
selectors and map modes have (missing attribute treated as `false` / field dropped) works because the controller
loops in Go and calls CEL once per element, so a runtime error scopes to one iteration and the loop moves on.
`extract.expression` is a single CEL evaluation over the whole `components` list; the controller has no
per-element boundary at which to catch and continue. Guard traversals of not-always-present fields with `has(...)`
or CEL optional access (`foo.?bar.orValue(...)`).

`Extract` is optional. When absent, the v2 descriptors are emitted, subject to the etcd soft limit.

#### No `Interval`: watch-driven reconciliation

Discovery has no periodic reconcile. Re-reconciliation is driven by:

- Discovery-generation change on the CR itself.
- Change to the referenced Component's resolved version, effective OCM config, or readiness state, mapped through a
  field-indexed lookup back to every Discovery referencing that Component.

Discovery derives entirely from upstream state (the referenced Component and the repository it resolves). Every
upstream change that could affect the output is already delivered by the existing watches, so an `Interval` would
only paper over gaps in the event source.

### Status reasoning

#### Split status: `components` and `extracted`

```go
// +optional
Components []apiextensionsv1.JSON `json:"components,omitempty"`
// +optional
Extracted []map[string]apiextensionsv1.JSON `json:"extracted,omitempty"`
```

Exactly one of the two is ever written. `spec.extract` set writes `extracted`. `spec.extract` absent writes
`components`.

Consumers only ever read one or the other.

A CRD-level validation can enforce them being mutually exclusive:

```yaml
x-kubernetes-validations:
  - rule: 'has(self.spec.extract) ? !has(self.status.components) : !has(self.status.extracted)'
    message: exactly one of status.components / status.extracted, keyed by spec.extract
```

`components` holds the selector output, one v2 descriptor per element:

- no selectors: every reachable descriptor.
- `referenceSelector`: target descriptors (root excluded; it has no incoming reference).
- `componentSelector`: matched descriptors, running on `referenceSelector`'s survivors.
- `resourceSelector`: descriptors with `resources` filtered.

To get the descriptor:

```go
var d v2.Descriptor
json.Unmarshal(item.Raw, &d)
```

`resourceSelector` filters the resource list, so an entry only matches the stored descriptor when no
`resourceSelector` is set. Do not verify signatures against this field. Use the repository.

##### No Typed Descriptors

`v2.Descriptor` cannot be a typed CRD field.

- `runtime.Type` has bare `Version` and `Name` fields with no JSON tags.
- `runtime.Raw.Data` is `json:"-"`

`extracted`: holds the output of the three `extract` modes, one record per emitted element. `extract.expression`
must return a list of objects. Any other return type is `ExtractFailed`.

A failed extract leaves the last successful payload in place and reports `Ready=False` / `ExtractFailed`.

#### `unresolved` and `spec.onResolutionError`

A reference can be reachable in the graph and still not resolve: it dangles, the credentials in the root's
`effectiveOCMConfig` do not cover it, or the target was transferred and is only reachable through a
`repositoryContexts` entry the controller does not consult (see [`componentRef`](#componentref)). On a large umbrella
graph this stops being an edge case.

`spec.onResolutionError` decides the contract:

- `Fail` (default): the reconcile aborts, the status payload is left untouched, and `Ready=False` /
  `ResolutionFailed` carries the first failure. Fail-closed is the default because a silently short list that a
  downstream controller turns into deployments is worse than a loud error.
- `Ignore`: the components that did resolve are emitted, and every failure is recorded in `status.unresolved`.
  `Ready=True` / `PartiallyResolved` with the count in the message.

```go
type UnresolvedReference struct {
    Component string `json:"component"`
    Version   string `json:"version"`
    Reason    string `json:"reason"`
    Message   string `json:"message,omitempty"`
}
```

`unresolved` is a typed, controller-owned sibling rather than entries inside `discovery`. Mixing the two would
collide with user-chosen `extract` field names, and a failed node has no descriptor, so its record would carry no
user fields at all and every consumer iterating `discovery` would have to defend against half-records.

#### etcd size

Status payloads are subject to the etcd soft limit (~1.5 MiB). Discovery does not store descriptors outside etcd.
Selectors and `Extract` let users shape the payload down. When the result still exceeds the limit, the API server
rejects the status write. The stored object still holds the previous payload, which was small enough, so the
controller leaves it alone, patches a `Ready=False` / `PayloadTooLarge` condition with the API server's rejection
message, and returns without requeueing. Same rule as a failed extract: keep the last good payload and report the
failure in the condition.

The large-descriptor navigation case is descoped. Out-of-status storage (encoded payload,
sibling ConfigMap, artifact CR) is an additive path if a concrete need appears.

#### Deterministic ordering: `(name, version)` lexicographic

Descriptors are sorted by `(component.name, component.version)` before `Extract` runs. Without this, the map iteration
order of the underlying graph store would make the status payload flap on every reconcile, causing watcher churn on
downstream consumers and generating unnecessary etcd writes.

#### Conditions and reasons

Discovery uses the shared condition types and reasons, plus Discovery-specific reasons:

- `SelectorFailed`: a `spec` selector failed to compile or evaluate.
- `ExtractFailed`: a `spec.extract` expression failed to compile or evaluate, or returned a type other than a list
  of objects.
- `ResolutionFailed`: a reachable reference did not resolve and `spec.onResolutionError` is `Fail`.
- `PartiallyResolved`: emitted on `Ready=True` when `spec.onResolutionError` is `Ignore` and `status.unresolved` is
  non-empty.
- `PayloadTooLarge`: the status write exceeded the etcd limit (see [etcd size](#etcd-size)).
- `NoReferencesMatched` and `NoComponentsMatched`: emitted on `Ready=True` when a selector filters the
  descriptor set to empty. In that state the field `spec.extract` selects is set to an explicit empty array (`[]`) so
  consumers can distinguish "query ran and matched nothing" from "payload never computed". Distinct reasons per
  stage so consumers can distinguish "no reference matched" from "reference matched but the component didn't".

Consumers of the status payload follow `status.effectiveOCMConfig` when they need credentials (pull-secrets, etc.)
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

- One CRD, three filter kinds, one expression language.
- Uses CEL, already in the module.
- Status shape is keyed by `spec.extract` and enforced by a CRD rule
- `components` hands back stored descriptors: one `json.Unmarshal` into `v2.Descriptor`, no conversion layer.
- Selector shape is reusable for `ResourceID.BySelector`.
- Watch-driven; no additional polling load.

Cons:

- Larger schema and larger conceptual surface than a specialised, single-shape CRD.
- No server-side validation.
- `resourceSelector` filters resources, so `components` cannot be used to verify signatures.
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
  `Ready=False` with reason `SelectorFailed` (selector overrun) or `ExtractFailed` (extract overrun), and, when
  non-retriable, also `Stalled=True`, instead of runaway CPU.
  Needs benchmark input to pick sensible defaults per binding scope.
- Unify `Selector` with `ResourceID.BySelector`: Once
  [ocm-project#296](https://github.com/open-component-model/ocm-project/issues/296) lands, verify the same
  `Selector` shape serves both consumers without further extension. Any divergence should be resolved on the
  shared type.
- Multi-root `componentRef`: A single Discovery CR targets one root Component today. Users needing an n:m shape
  compose it with multiple Discovery CRs or an umbrella-of-umbrellas Component. We might think of a
  `componentRefs []ComponentRef` in the future.
- Per-descriptor digest verification: Recompute each transitively-resolved descriptor's digest and compare
  against the digest asserted by its parent reference; a mismatch is a resolution failure and lands in
  `status.unresolved` with a `DigestMismatch` reason, so no room needs to be reserved in the status payload.
  Not shipped: recomputing digests on every reconcile is expensive on large graphs. Open question whether to enable
  this later as an opt-in flag.

## Conclusion

Discovery ships as one CRD that projects a filtered slice of a component's transitive reference graph into a
status payload: typed v2 descriptors when no `extract` is set, free-form records when one is. It targets
OpenControlPlane's shape and leaves ODG's synchronous-access needs out of scope. Caching is delegated to the component descriptor cache
landing upstream in PR #2833.
