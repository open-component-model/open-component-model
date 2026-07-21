# Component and Resource Discovery in the OCM Kubernetes Controller Toolkit

* **Status**: proposed
* **Deciders**: @frewilhelm @fabianburth
* **Date**: 2026-07-07

Technical Story:

- EPIC: [ocm-project#1153](https://github.com/open-component-model/ocm-project/issues/1153)
- Spike: [ocm-project#1154](https://github.com/open-component-model/ocm-project/issues/1154)

## Context and Problem Statement

The OCM Kubernetes Controller Toolkit currently exposes an address-based consumption model: a user creates a
`Repository`, then a `Component` pinned to one semver constraint, then one `Resource` per artifact they want to consume.
Every level of that chain requires the caller to know the exact identity of what they are pulling:
repository spec, component name, resource identity, and (for nested references) the full `referencePath`.
See `kubernetes/controller/api/v1alpha1/{repository,component,resource}_types.go`.

Two external consumer projects have asked for a query-based mode on top of that address-based baseline:

- **ODG**, the [Open Delivery Gear](https://github.com/open-component-model/odg-core) project, needs to resolve
component descriptors on demand, enumerate the versions available for a component,
filter by publication date, and download resources for external scanners (SBOM, vulnerability tooling).
- **OpenControlPlane**, the [openmcp-project](https://github.com/openmcp-project) org, needs a single custom resource
to span multiple umbrella components (`n:m`, not `1:1`), match resources by selector, and propagate pull-secrets into
the resulting sub-resources.

Neither workflow is expressible today. A caller who does not know the exact
`(component, version, resource, referencePath)` tuple up front has no way to ask the controller
"give me all versions of resource R across umbrella C's references" or
"publish this component's descriptor so I can navigate it".

### What prompted this

ODG and OpenControlPlane both approached the toolkit maintainers with concrete workflows blocked on the current
address-only model. ODG's use case is scanning: pipelines resolve a component version, walk its references recursively,
and hand each resource to a scanner. The address model forces ODG to synthesise a fresh `Component` and `Resource`
CR per hop and wait for the reconciler to settle, which their synchronous callers probably cannot afford.

OpenControlPlane's use case is umbrella-component consumption: one delivery bundle references many sub-components,
and the operator wants one custom resource to describe "the FluxCD chart across every environment component in this
bundle" rather than N handwritten `Resource` CRs.
Details in [ocm-project#1153](https://github.com/open-component-model/ocm-project/issues/1153) and the requirement
comments on [ocm-project#1154](https://github.com/open-component-model/ocm-project/issues/1154).

### Constraints

- Kubernetes' etcd stores objects with a soft limit of [~1.5 MiB](https://etcd.io/docs/v3.6/dev-guide/limit/) per
resource. Component descriptors can approach or exceed this on their own:
`europe-docker.pkg.dev/gardener-project/releases//github.com/gardenlinux/gardenlinux:2150.6.0` is ~884 KB compact JSON
and ~37 000 lines rendered as YAML. A design that embeds full descriptors in `.status` is not viable for real workloads.
- The async reconcile round-trip (`create CR` -> wait for status -> read result) is a UX blocker for ODG's synchronous
callers. Any CRD-only surface must be paired with a synchronous access path, or ODG will need to bypass the controller.
- The existing `Repository`, `Component`, and `Resource` CRDs are already in use by ArgoCD/FluxCD integrations via kro
`ResourceGraphDefinition`s. Discovery must not break the current API contract.
- Discovery reuses the existing credential story: `OCMConfig` propagated through `Repository`. No new credential fields
on discover CRs.

## Requirements

Requirements are written as normative statements using RFC 2119 keywords, so later revisions can score the options
against them. IDs are stable and citable from PRs and reviews. Requirements are grouped into a shared block
(both stakeholders agreed) and per-stakeholder blocks (asked for by one, not contested by the other).

### Shared requirements

- **R-S1** The discovery surface MUST support selector-based resource addressing (matching by criteria, not by exact
identity).
- **R-S2** The discovery surface MUST publish multi-result responses. Result shapes such as `status.matches[]` or
`status.versions[]` are expected; the existing single-valued `status.resource` / `status.component` is not sufficient.
- **R-S3** The discovery surface MUST expose component descriptors for client-side navigation. Given the etcd size
constraint, "expose" MAY mean an encoded and/or zipped string or even a persistent storage location.
- **R-S4** The discovery surface MUST support recursive traversal of component references without requiring the caller
to enumerate `referencePath` by hand.
- **R-S5** The discovery surface MUST reuse the existing `Repository` and `OCMConfig` mechanisms for repository access
and credentials. Separate credential fields on discover CRs are NOT allowed.

### ODG-specific requirements

- **R-O1** The discovery surface SHOULD support custom repository lookup based on regex patterns over component name
and version, mirroring the ODG bootstrapping mechanism (see 
[`odg-core values.documentation.yaml`](https://github.com/open-component-model/odg-core/blob/e650ac282e22d74053f96b189df2a134f68f4906/charts/bootstrapping/values.documentation.yaml#L765)).
- **R-O2** The discovery surface MUST offer a way to enumerate the versions available for a given component name and
MUST accept a date range and return only versions whose creation date falls inside it.
- **R-O3** The discovery surface SHOULD provide a resource-download entrypoint that external scanners (Python-based
SBOM and CVE tooling).
- **R-O4** The discovery surface SHOULD provide a synchronous-friendly access path in addition to CRDs. A library
binding or an in-process API endpoint would satisfy this; async CR reconcile alone does not.

### OpenControlPlane-specific requirements

- **R-C1** A single discovery CR MUST be able to target multiple umbrella components (`n:m` relationship), not just one.
- **R-C2** Discovered sub-resources MUST have pull-secrets propagated into their status, mirroring how the existing
`Resource` CR surfaces pull-secrets. (needs [clarification](https://github.com/open-component-model/ocm-project/issues/1154#issuecomment-4912202880))

## Considered Options

The three CRD sketches from [ocm-project#1153](https://github.com/open-component-model/ocm-project/issues/1153) are
reproduced below for reference. A fourth option (a single merged `Discover` CRD) is listed without a sketch.
Pros, cons, and a Decision Outcome are deferred to a follow-up revision of this ADR.

### Option 1: `ResourceRange`

Takes one component name plus a resource name, optionally scoped to component references matching a substring.
Publishes the list of resource versions found and their access locations.

```yaml
kind: ResourceRange
spec:
  compRef: releasechannel
  resource:
    name: flux-chart
    componentReference: flux
status:
  versions:
    - name: v2.2.0
      imageReference: chart-url:2.2.0
    - name: v2.1.0
      imageReference: chart-url:2.1.0
```

### Option 2: `ResourceSet`

Takes a resource name and a set of components identified by selector. Publishes the resources found across all
matching component versions.

```yaml
kind: ResourceSet
spec:
  name: chart
  components:
    - bySelector:
      name: ghcr.io/open-component-model/flux
  versions:
    - name: v2.2.0
      imageReference: chart-url:2.2.0
    - name: v2.1.0
      imageReference: chart-url:2.1.0
```

### Option 3: `ComponentDescriptor`

Takes one component name and publishes its component descriptor in the status, optionally filtered by `jsonPath`.

```yaml
kind: ComponentDescriptor
spec:
  compRef: flux
  jsonPath: ...
status:
  ...
```

### Option 4: ???


## Open Questions

- Should discovery produce downstream `Component` / `Resource` CRs (write path) or only publish a read-only status.
- Does the synchronous-access requirement (R-O4) force a non-CRD surface (gRPC / HTTP endpoint), or is a
well-warmed cache in the `internal/resolution/` service sufficient for ODG's callers?
  - If we cannot solve this, the ODG requirements may be impossible to satisfy without bypassing the controller. Hence,
not relevant for this ADR.
