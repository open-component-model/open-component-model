# OCM-native deployment wrappers: thin per-engine CRs over Flux/Argo

* **Status**: proposed
* **Deciders**: @open-component-model/Maintainers
* **Date**: 2026-11-05

Technical Story: OCM needs a simple entry point for delivery. This ADR defines three thin per-engine
wrapper CRs that resolve an OCM component version as the source of a Flux `HelmRelease`, a Flux
`Kustomization`, or an Argo CD `Application`, and apply OCM localization and configuration.

## Context and Problem Statement

Deploying a Helm chart or kustomization from an OCM component today needs an orchestration layer (a
Kro `ResourceGraphDefinition` or a Crossplane Composition) that wires `Resource`, `OCIRepository`, and
`HelmRelease`/`Kustomization` (or `Application`) objects plus localization and configuration. A
definition is reused by small instances, but someone has to write one per chart or component shape: the
[`helm-configuration-localization`](../../bindings/go/kubernetes/controller/examples/helm-configuration-localization/rgd.yaml)
example needs about 120 lines for one chart and one image (Flux and Argo variants), and every further
image adds a `Resource` and a hand-written mapping onto the chart's values paths. The platform also has
to run Kro or Crossplane.

## Decision Drivers

* Users keep the full API of their deployment engine, including fields added in later engine releases,
  without waiting for an OCM release.
* OCM owns only what it uniquely knows (component resolution and verification, localization,
  configuration); engine behavior stays with the engine.
* Deploying a chart or kustomization from a component takes one object, without a hand-written
  definition per chart or an extra orchestration layer.
* Images are localized after transfer from data in the signed component version, without hand-written
  mappings, and pinned to their digests where the chart allows it.

## Considered Options

1. **Status quo**: `Repository`, `Component`, and `Resource`, wired to Flux/Argo by a Kro or Crossplane
   definition (optionally shipped in the component and deployed through a `NamespacedDeployer`).
2. **CEL-chained single CR**: one resource whose spec chains CEL functions that create child CRs.
3. **Typed engine-neutral CR**: one versioned spec exposing a curated subset of Flux/Argo fields.
4. **Per-engine wrappers**: one thin CR per engine object that passes the native spec through and
   replaces only its source with an OCM source.

## Decision Outcome

Chosen option 4, per-engine wrappers.

Justification:

* Users write the engine spec they already know. Nothing is curated, so no field goes missing
  (option 3), and fields an engine adds to the rendered API version need no OCM change.
* The OCM value-add is isolated in one controller path.
* One CR with the native engine spec replaces a hand-written definition per chart (option 1), with
  plain Kubernetes types instead of the CEL dialect of option 2.

### API

Three namespaced CRs in `delivery.ocm.software/v1alpha1`:

| Wrapper | Renders | Controller sets | `template` must not set |
| --- | --- | --- | --- |
| `OCMHelmRelease` | Flux `helm.toolkit.fluxcd.io/v2` `HelmRelease` and its source | `spec.chartRef` or `spec.chart`; merged `spec.values` | `spec.chartRef`, `spec.chart` |
| `OCMKustomization` | Flux `kustomize.toolkit.fluxcd.io/v1` `Kustomization` and its `OCIRepository` | `spec.sourceRef`; merged `spec.images`, `spec.patches` | `spec.sourceRef` |
| `OCMApplication` | Argo `argoproj.io/v1alpha1` `Application` | `spec.source.repoURL`, `targetRevision`, and `chart` (HTTP Helm) or `path` (OCI); merged `helm.valuesObject` or `kustomize.images`/`patches` | `spec.source.repoURL`, `chart`, `targetRevision`, `helm.values`, `path` for a Helm chart; `spec.sources`, `spec.sourceHydrator` |

Because `spec.chart` is reserved, chart-template options such as `spec.chart.spec.valuesFiles` are not
available; `configResources` cover that need.

All three share these fields:

| Field | Meaning |
| --- | --- |
| `source` | OCM coordinates: `repositorySpec`, `component`, `semver` (exact or range), optional `semverFilter`, `downgradePolicy` (default `Deny`), `interval` (polling of the child `Component`), `resource` (the deploy artifact, a `ResourceReference`), `verificationPolicy` (default `Always`). |
| `template` | The engine object as JSON: `spec` verbatim minus the keys above, plus optional `metadata.labels`, `annotations`, and `finalizers`. Other `metadata` keys, including `name` and `namespace`, are rejected. |
| `configResources[]` | `{byReference, verificationPolicy}`: OCM resources that supply Helm values or Kustomize patches. |
| `applyServiceAccountName` | Service account the controller impersonates for every apply; default `default`. |
| `deletionPolicy` | `Background` (default), `Foreground`, or `Orphan`, for the generated objects. |
| `ocmConfig` | OCM configuration for the child OCM CRs, such as credentials and signature verification. |
| `interval` | Re-apply cadence of the wrapper. |

The two Flux wrappers also carry an optional `sourceTemplate`: the Flux source object, handled like
`template` (JSON, rendered as `unstructured`) plus a required `kind`: `OCIRepository` for an OCI
artifact, including a `Helm/v1` chart in an `oci://` repository, and `HelmRepository` for a `Helm/v1`
chart in an HTTP repository. The controller sets `spec.url` and, for `OCIRepository`, `spec.ref` (the
signed digest, or the chart version where the Source table pins by version), and fills a missing
`spec.interval`, which Flux requires, from the wrapper's `interval`; `verify`, `secretRef`, `provider`,
`layerSelector`, and every other field are the user's. A `kind` that does not fit the deploy resource's
access, or a template that sets `url` or `ref`, is rejected with `UnsupportedConfiguration`. Without
`sourceTemplate`, the controller renders the fitting kind with only these fields. Argo CD has no source
object, so `OCMApplication` has no such field.

The engine object takes the wrapper's name. Every other generated object is named
`<name>-<kind>-<role>`, with `hr`, `ks`, or `app` for the wrapper kind (e.g. `podinfo-hr-source`,
`podinfo-hr-component`), so wrappers of different kinds can share a name.

Status carries the `Ready` condition plus `observedGeneration`, `effectiveOCMConfig`, `deployed` (the
generated objects), `deployedComponentVersion`, `deployedResourceDigest`, and `appliedLocalizations[]`.

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: OCMHelmRelease
metadata:
  name: podinfo
  namespace: team-a
spec:
  interval: 10m
  source:
    repositorySpec: {type: OCIRepository/v1, baseUrl: registry.example.com, subPath: ocm}
    component: ocm.software/podinfo
    semver: ">=6.9.0"
    interval: 10m
    resource:
      resource: {name: chart}
  sourceTemplate:
    kind: OCIRepository
    spec:
      secretRef: {name: registry-pull}
  configResources:
  - byReference:
      resource: {name: production-values}
  template:
    spec:
      interval: 10m
      install: {remediation: {retries: 3}}
      values: {replicaCount: 2}
```

### Template handling

`template` is stored as JSON (`Type=object`, `x-kubernetes-preserve-unknown-fields`). The controller
copies it, sets `apiVersion`, `kind`, `metadata.name`, and `metadata.namespace`, injects the OCM source
and the merged configuration, and passes every other field through untouched.

* **Native fields** are validated by the engine's CRD when the controller applies the rendered object.
  Server-side apply always validates against the schema, so a typo surfaces as `Ready=False`.
* **Reserved keys** (table above) are checked by the controller: `Ready=False`, reason
  `UnsupportedConfiguration`. They cannot be CEL rules on the wrapper CRD, because CEL cannot see fields
  kept by `x-kubernetes-preserve-unknown-fields`. Argo's `spec.sources` and `spec.sourceHydrator` are
  reserved because Argo ignores `spec.source` when either is set; `helm.values` because Argo ignores it
  when `valuesObject` is set.
* **JSON, not embedded Flux/Argo Go types.** A typed embed generates the wrapper's CRD schema from the
  vendored engine version, so the API server would prune any field a newer installed engine supports.
  It would also pull Flux and Argo modules into `bindings/go`, which the library and CLI share;
  `argoproj/argo-cd` additionally requires inheriting `replace` directives that can conflict with OCM's
  pinned `k8s.io` versions.

The controller renders `unstructured` objects with fixed apiVersions: Flux `source.toolkit.fluxcd.io/v1`,
`helm.toolkit.fluxcd.io/v2`, and `kustomize.toolkit.fluxcd.io/v1` (floor Flux 2.6.0); Argo
`argoproj.io/v1alpha1` (floor Argo CD 3.1.0, where OCI sources are beta; 3.5 for plain-HTTP OCI via
`insecureOCIForceHttp`). Floors are not enforced; a missing CRD surfaces as `Ready=False`. Moving to a
new engine API version needs an OCM release.

### Source

| Deploy resource access | Flux source kind | Engine reference |
| --- | --- | --- |
| OCI, or `LocalBlob` stored as an OCI manifest | `OCIRepository`, `ref.digest` from the signed descriptor | Helm `spec.chartRef`; Kustomize `spec.sourceRef`; Argo `repoURL: oci://...`, `targetRevision: <digest>` |
| `Helm/v1` with an `oci://` repository | as OCI; pinned by digest when the descriptor digest names the chart manifest, else by version | as OCI |
| `Helm/v1` with an HTTP repository | `HelmRepository` | Helm `spec.chart.spec` (`chart`, `version`, `sourceRef`); Argo `repoURL: https://...`, `chart`, `targetRevision: <version>` |

* HTTP `Helm/v1` charts are pulled by name and version, an accepted integrity opt-out.
* An engine can pull a `LocalBlob` only if OCM stored it as an OCI manifest (an OCI-layout blob) in an
  OCI registry. A plain blob, such as the default `dir` input tar, is stored as a single layer that no
  engine can reference by digest; it is rejected with `UnsupportedConfiguration`.
* The artifact must fit the engine: Flux extracts the first layer unless
  `sourceTemplate.spec.layerSelector` selects another; Argo CD expects a single layer of a supported
  media type.
* Argo requires `spec.source.path` for an OCI source: the controller sets `.` for a Helm chart, while a
  kustomization's `template` may name the directory inside the artifact (default `.`).
* The deploy resource is a Helm chart if its access is `Helm/v1`, its media type is a Helm chart type,
  or its type is `helmChart`; anything else is a kustomization. A shape the wrapper cannot render (a
  chart for `OCMKustomization`, a kustomization for `OCMHelmRelease`) is rejected with
  `UnsupportedConfiguration`.

Each consumer authenticates on its own: child OCM CRs through `ocmConfig`, the Flux source through
`sourceTemplate.spec` (`secretRef`, `provider`), Argo CD through repository credentials in its own
namespace (operator-side, outside the wrapper), and the workload through image pull secrets. For an OCI
registry, one `.dockerconfigjson` Secret can serve both `ocmConfig` and `sourceTemplate.spec.secretRef`.

### Localization

`ocm add cv --localize-hints` writes a signed `ocm.software/localization` label on every resource of
type `ociImage`, taken from its image reference; hand-written labels always win. An image added by
value, such as an OCI layout added through an input, has no such reference: it is stored as a
`LocalBlob` whose access records only its digest and media type, not the name the chart uses for it.
Neither the CLI nor the controller can then tell which values entry it belongs to, so the CLI warns and
the label has to be written by hand. The label records only *which* image a resource is, as its
pre-transfer coordinates, never *where* it appears in a chart, so signatures never depend on chart
structure. Transfer rewrites `access`, which is not signed; the label keeps the original coordinates.

```yaml
labels:
- name: ocm.software/localization
  version: v1
  signing: true
  value:
    type: ociImage
    matchKey:
      registry: ghcr.io
      repository: stefanprodan/podinfo
      tag: 6.9.1
      digest: sha256:262578...   # omitted with --skip-reference-digest-processing
```

At deploy time a label matches an image reference on registry and repository only (Docker Hub short
names normalized), so a chart-side tag bump still matches. The substitute comes from the matched image's
child `Resource` status (post-transfer location and digest, checked against the descriptor, whose
signature the child `Component` verifies when `ocmConfig` configures it), falling back to
`matchKey.digest`.

* **Helm**: the controller walks the default values of the chart and of every vendored subchart,
  recursively. A subchart's effective defaults are its own `values.yaml` overlaid with the parent's
  section under the subchart's alias (or name), as Helm computes them. Matches are written under that
  path (`postgresql.image`, nested `a.b.image`), which Helm hands to the subchart. Subcharts are walked
  whether or not a `condition` or `tags` enables them: enablement depends on the final values, and Helm
  ignores values for a disabled subchart. An image reference is a scalar OCI string or a map with
  `repository`/`image` and optional `registry`, `tag`, `digest`. A child image `Resource` is created only
  for referenced images.
* **Kustomize**: one `images` entry per labelled image (`name` to `newName` plus `newTag` or `digest`;
  Flux takes objects, Argo shorthand strings). Kustomize matches by name, so every labelled image gets a
  child `Resource`; an unused one costs a metadata resolve. The match is literal and the controller does
  not read the manifests, so a Docker Hub image gets one entry per common spelling (`nginx`,
  `library/nginx`, `docker.io/nginx`, `docker.io/library/nginx`); entries that match nothing are no-ops.

A reference whose chart value has no place for a digest gets the tag, a warning Event, and
`appliedLocalizations[].digestPinned: false`. If several labels match one reference and relocate to
different locations, the controller cannot choose: `Ready=False`, `UnsupportedConfiguration`, unless a
higher layer replaces that image.

### Configuration and merge order

Both engines use one precedence, lowest first: chart or kustomization defaults, localization,
`configResources` in spec order, `template`. The one exception is a localized image in a kustomization,
below.

* **Helm**: the controller merges into `spec.values` (Argo: `helm.valuesObject`); the values in
  `template` are extracted as the top layer. Maps deep-merge, arrays replace. The coordinates of a
  localized image block are atomic: a higher layer that sets `repository` or `image` replaces them all
  (the escape hatch for a wrong match), while one that sets only `registry`, `tag`, or `digest` is
  rejected with `UnsupportedConfiguration`, because it cannot apply coherently to a relocated,
  digest-pinned image. Other keys of the block, such as `pullPolicy`, merge normally. Native value
  sources keep their engine semantics: Flux `valuesFrom` sits below the merged inline values, except
  entries with `targetPath`, which Flux applies on top; Argo `helm.parameters` override everything, and
  `valueFiles` sit below `valuesObject`.
* **Kustomize**: localization goes into `images`, configuration into `patches`
  (`Kustomization.spec.patches`, `Application.spec.source.kustomize.patches`). Each `configResources`
  entry supplies a top-level `patches` list (`{patch, target}`); a missing or invalid list is rejected
  with `UnsupportedConfiguration`. The controller emits `configResources` patches in spec order, then
  the `template` patches, so `template` applies last. Kustomize runs its `images` transformer after all
  patches, so localization would silently rewrite a patched image. A patch, from `configResources` or
  `template`, that sets or may set an image that localization matches is therefore rejected with
  `UnsupportedConfiguration`; a `template` `images` entry, which replaces the localized entry with the same
  `name`, is the only way to change a localized image.

### Reconcile flow

Every reconcile runs all steps; "ensure" means create-or-update.

1. Check the reserved `template` keys. Ensure the child `Repository`, `Component`, and deploy
   `Resource`; wait until the deploy `Resource` is verified. Its access selects the source shape.
2. Helm only: download the chart (digest-cached, size-capped by a controller flag, default `5Mi`) and
   collect image references from the chart and its subcharts. Kustomizations are not downloaded.
3. Ensure one image `Resource` per matching label (pruning the rest) and one `Resource` per
   `configResources` entry; wait until they are ready.
4. Merge values, images, and patches in the order above.
5. Render the engine object (and, for Flux, the source from `sourceTemplate`), impersonate-apply both
   through `applyset`, and roll child and engine readiness into `Ready`.

The watch on owned `Resource`s enqueues the wrapper on create, delete, a `Ready` transition, or a change
of the resolved resource (`status.resource`, e.g. a new digest), not on other status writes, so N image
`Resource`s cause O(N) wakeups, not O(N²).

### Identity, deletion, and status

The controller impersonates `applyServiceAccountName` (default: the namespace's `default` service
account) for every apply. That account bounds what the wrapper creates, not what the engine deploys:
the engine applies the workload under its own identity (Flux: `template.spec.serviceAccountName`, else
Flux's default; Argo CD: its controller, scoped by the `AppProject`). As with `NamespacedDeployer`,
whoever may create a wrapper can have the controller act as any service account in its namespace; the
author's own RBAC does not bound this, so the right to create wrappers in a namespace amounts to trust
in every service account there. Controller RBAC: `get;list;patch;watch` on the engine and Flux source
kinds and `impersonate` on service accounts. No `Namespace` is rendered; the target namespace is a
native `template` field (`spec.targetNamespace`, `spec.destination.namespace`).

Child OCM CRs are owner-referenced and garbage-collected with the wrapper. The generated source and
engine object are `applyset` members: `Background` and `Foreground` prune them; `Orphan` removes the
`applyset.kubernetes.io/part-of` label and owner reference and leaves them for adoption. If the apply
service account is gone, deletion behaves as for `NamespacedDeployer`: pruning falls back to
owner-reference garbage collection, and `Orphan` waits for the service account unless the namespace is
terminating.

The controller never injects engine teardown settings. What happens to the workload when the engine
object is deleted, by pruning now or by an adopter later, is set in `template`:

* **Flux `Kustomization`**: its own `spec.deletionPolicy`. The default `MirrorPrune` deletes the
  workload when `spec.prune` is `true`; `Orphan` keeps it.
* **Flux `HelmRelease`**: deletion runs a Helm uninstall unless the object is suspended.
  `uninstall.deletionPropagation: orphan` does not prevent this; it only orphans dependents such as
  ReplicaSets and Pods, while the Deployment and Service are still deleted.
* **Argo `Application`**: the workload is deleted only if `template.metadata.finalizers` contains
  `resources-finalizer.argocd.argoproj.io`. Without it, even `Background` leaves the workload running.

`Ready` is `True` when the children are ready and the engine reports the rendered state as healthy:
for each Flux object, `Ready=True` with `status.observedGeneration` equal to `metadata.generation`, and
for an engine object fed by an `OCIRepository`, the pinned digest reported as applied
(`Kustomization` `status.lastAppliedRevision`; `HelmRelease` `status.history[0]` with `status: deployed`
and `ociDigest` equal to the pinned digest), because a new digest changes only the source, not the
engine object's spec. The `HelmRelease` check relies on helm-controller's digest tracking, which its
`DisableChartDigestTracking` feature gate turns off. For Argo, the `Application` must be `Healthy` and
`Synced`, with `status.sync.comparedTo.source` and `.destination` equal to the rendered ones and no
operation running. Without these checks, a status left from the previous generation would mark a new
component version ready before the engine has acted on it; `deployedComponentVersion` and
`deployedResourceDigest` change only once this holds. A child that never becomes ready is not terminal;
the wrapper requeues. An `Application` without status usually means Argo CD does not watch the namespace
(apps-in-any-namespace); the condition message says so.

## Pros and Cons of the Options

### Option 1: Status quo

* Pro: works today; any wiring is possible; deployments are small instances of a reusable definition,
  and one definition can deploy several charts from one `Component`.
* Con: a definition per chart or component shape, with every image mapped by hand onto the chart's
  values paths; needs Kro or Crossplane.

### Option 2: CEL-chained single CR

* Pro: one resource, no orchestration layer.
* Con: rejected after user feedback
  ([ocm-project#1010](https://github.com/open-component-model/ocm-project/issues/1010)): the CEL dialect
  is undiscoverable from the Kubernetes API, embeds registry URLs at definition time, and still leaves
  localization and configuration to the user.

### Option 3: Typed engine-neutral CR

* Pro: one CR for all engines; every field is validated at admission.
* Con: the curated field set is incomplete by design, so users needing an unmodelled field are stuck;
  full coverage re-declares each engine's API and needs an OCM release for every engine change.

### Option 4: Per-engine wrappers (chosen)

* Pro: the full native API, including fields an engine adds to the rendered API version, without an
  OCM release.
* Pro: the OCM value-add is isolated; the user applies one CR and the controller owns everything else.
* Con: three CRs; a new engine needs a new wrapper and render path. Only Flux and Argo CD are supported.
* Con: one wrapper deploys one resource; a component with several charts needs one wrapper per chart,
  each resolving the version on its own.
* Con: `template` errors surface as `Ready=False` after apply, not at admission.
* Con: only images exposed in chart values or as labels for Kustomize are localized. Images hard-coded
  in templates, or composed from values the walker does not resolve (for example a `global.*` registry
  that a template prefers over the image block), are not.
* Con: credentials are configured per consumer (`ocmConfig`, `sourceTemplate`, Argo CD repository
  configuration, workload pull secrets).
