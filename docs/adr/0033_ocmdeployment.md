# OCMDeployment: single-resource deployment with construction-derived localization

* **Status**: proposed
* **Deciders**: @open-component-model/Maintainers
* **Date**: 2026-10-27

Technical Story: OCM needs a simplified entry point for delivery. This ADR defines
the `OCMDeployment` resource and the contract by which it localizes and configures a Helm chart or
kustomization delivered in a component version. The status quo it improves on is described below.

## Context and Problem Statement

Deploying a Helm chart or kustomization that lives in an OCM component today requires
an orchestration layer (e.g. a Kro `ResourceGraphDefinition` or a Crossplane Composition) that wires
a `Resource`, an `OCIRepository`, and a `HelmRelease` or `Kustomization` (or an Argo `Application`),
and encodes localization and configuration (see
[`helm-configuration-localization/rgd.yaml`](../../bindings/go/kubernetes/controller/examples/helm-configuration-localization/rgd.yaml)
— around 130 lines for one chart plus one image). The deployer technology (Flux vs Argo) leaks into what the
component author bundles, and too many cluster resources are needed to deploy what is essentially a
chart plus its images.

A single applied resource should name a component version, the resource to deploy, a deployer engine,
and a config, and have the controller produce the low-level deploy objects with images localized and
values configured, without an orchestration layer.

`OCMDeployment` is an addition, not a replacement. It renders the low-level Flux/Argo objects
directly and applies them itself; it does not route through the `NamespacedDeployer` CR. The
`NamespacedDeployer` consumes an OCM resource and applies its decoded contents verbatim;
`OCMDeployment`'s objects are rendered in-process (localization via signed labels, image-coordinate
resolution from child `Resource` CRs, and config/values merge) and exist only in memory — there is
no OCM resource to point a `ResourceRef` at, and a rendered `HelmRelease`/`Application` is not an
OCM resource we would materialize just to re-download it. The apply/prune machinery is shared,
though: both controllers go through the same `applyset` utility (one field manager,
`delivery.ocm.software/applyset`) and an impersonating client; the orchestration around it should be
factored into a shared helper rather than kept as parallel copies. It also reuses shared API types
(`DeletionPolicy`, `DeployedObjectReference`) and the `Component`'s `DowngradePolicy`. Its primary
purpose is to migrate v1 users to v2 and lower the adoption bar for the common case. We accept
consciously that this is neither perfectly clean nor fully deployer-agnostic.

## Decision Drivers

* Single resource for the user to apply and maintain (the controller owns the generated objects).
* Lower the v1 to v2 migration and adoption bar for the common case.
* Images are localized automatically; the user never edits image references.
* Localization data must survive transfer and be tamper-evident where the chart permits it.
* Configuration can be shipped with the component and layered per deployment.

## Considered Options

1. **Status quo.** The user applies `Repository`, `Component`, `Resource`, a `NamespacedDeployer`, and an
   orchestration layer (e.g. Kro `ResourceGraphDefinition` or Crossplane Composition) that wires them
   to the Flux or Argo objects. Flexible, but every deployment repeats the same wiring, the engine
   leaks into what the component author bundles, and the up-front barrier (five resources plus an
   orchestration layer) is disproportionate for the common single-chart-plus-image case.
2. **CEL-chained `OCMDeployment`.** A single resource whose spec chains CEL functions (`repoByRef()`,
   `componentByReference()`, `resourceByIdentity()`) that implicitly create the child CRs. Rejected after
   user feedback ([ocm-project#1010](https://github.com/open-component-model/ocm-project/issues/1010)):
   the custom CEL dialect is not discoverable from the Kubernetes API, it replaces one form of wiring with
   another, and it forces authors to embed registry URLs at definition time — and it still leaves
   localization and configuration to the user, so the new abstraction does not lower the entry barrier
   enough to justify itself.
3. **Typed `OCMDeployment`.** This ADR. A single resource with a plain, versioned spec; the controller
   creates the child CRs and renders the engine objects.

## Decision Outcome

The `OCMDeployment` is a namespaced wrapper (orchestrating) custom resource
(`delivery.ocm.software/v1alpha1`, kind `OCMDeployment`): it is the only resource the user applies, and
its controller creates and owns the lower-level resources the user would otherwise have to define.
The controller stays thin: it creates the deploy-resource and config `Resource`s once, creates the
per-image `Resource`s for the localized images, and then reacts to their status. It
re-runs localization each reconcile and re-applies idempotently through the `applyset` machinery; the
recorded deploy-resource digest anchors a future re-match-gating optimization (not implemented in this
revision).

Localization is derived at construction and carried as signed per-resource labels; configuration and
localization are applied through the deployer's native mechanism (Helm values; kustomize `images`
for localization and `patches` for configuration).

### Reconcile flow

The steps below reference `spec` fields by path; the full
[`OCMDeploymentSpec`](#ocmdeployment-resource) struct is defined below.

The controller executes the following ordered steps on **every** reconcile, not once at creation.
Each step is idempotent: "create a `Resource`" means ensure it exists (create if absent, otherwise
update its spec to the current reference), and the controller re-derives localization and re-applies
the engine objects every pass (the chart download in step 3 is cached by digest, so it is not
repeated unless the resolved artifact changes).

1. Create a `Repository` from `spec.source.repositorySpec` and a `Component` from
   `spec.source.component`+`spec.source.semver`. These are ensured (created/updated) but not waited
   on here; the first readiness gate is the deploy `Resource` in step 2.
2. Create a `Resource` for the deploy resource (`spec.deployResource`); wait for its status to provide
   verified coordinates. The `Resource` controller resolves and verifies coordinates but does not
   download content. The deploy resource's access (where the chart or manifests physically live) can
   take two shapes, and the controller renders a matching source for each:
   - an **OCI access** (or a `LocalBlob`, which is reconstructed into an OCI digest reference) →
     rendered as an OCI source (`OCIRepository` for Flux, `oci://` for Argo);
   - a **`Helm/v1` access** — a chart that lives in a classic HTTP/HTTPS Helm repository (the
     component references it at its upstream location rather than copying it into OCM) → rendered as
     a native HTTP Helm source.

   See [the artifact matrix](#deployer-engine-and-artifact-matrix) for the full source/engine mapping.
3. For a Helm deploy resource, download the chart and read its default `values.yaml` to discover image
   references; downloads are size-capped by the `--ocmdeployment-download-max-resource-size` flag
   (default `5Mi` — a generous bound for whole deploy artifacts like Helm charts, above the general
   `2Mi` single-resource cap). For kustomize nothing is downloaded here — image localization is
   label-driven (see [Localization](#localization)), so the deploy artifact is left for the engine to
   pull.
4. Create a `Resource` for each localized image: for Helm only labelled `ociImage` resources
   referenced in `values.yaml`, for kustomize every labelled `ociImage`. Why the engines differ and
   why that is safe is in [Image Resource fan-out](#image-resource-fan-out-and-reconcile-cost).
   `Resource`s for images that no longer match are pruned; new matches are created. Wait for each to
   become ready — its status yields the relocated location used at render.
5. Create a `Resource` for each `spec.configResources` entry (config yaml blobs), wait for each to
   become ready, download their content, then merge values (Helm) or assemble the patch list
   (kustomize) — see [Configuration and merge order](#configuration-and-merge-order).
6. Render the engine object(s) and impersonate-apply via `applyset`
   (see [Apply identity and RBAC](#apply-identity-and-rbac)).
7. Roll child `Resource` and engine readiness into the `OCMDeployment`'s `Ready` condition
   (see [Status conditions](#status-conditions)).

### Apply identity and RBAC

The controller applies the generated deploy objects by **impersonating**
`spec.applyServiceAccountName` — the same model the merged `NamespacedDeployer` uses. It is optional
(defaults to the namespace `default` SA) and the apply never uses the controller's own identity.
Reading `ocmConfig` credentials and downloading content are outside the impersonated apply and follow
the other OCM CRs' behavior.

For Flux, two identities are involved, both resolved in the `OCMDeployment`'s namespace and both
defaulting to that namespace's `default` SA when unset:

* `applyServiceAccountName`: the controller impersonates this SA to create the Flux objects;
  it needs the engine-kind Role.
* `flux.serviceAccountName`: Flux impersonates this SA to apply the workload; it needs
  rights in `targetNamespace` (via a RoleBinding when `targetNamespace` differs). When `flux.kubeConfig`
  targets a remote cluster, this SA and its rights live on that remote cluster, not locally.

For ArgoCD the workload-sync identity defaults to the Argo CD control-plane SA (cluster-admin-level).
The operator narrows it (Argo CD 3.5+) by enabling `application.sync.impersonation.enabled` in
`argocd-cm` and setting `AppProject.spec.destinationServiceAccounts` (destination server+namespace →
SA); `argo.project` selects the `AppProject`. Without the flag, sync runs with broad control-plane rights.

Beyond the single `OCMDeployment`, an operator provisions ServiceAccounts once per namespace and the
controller needs `impersonate` on them. The one-resource claim is about the deploy itself, not this
one-time SA setup.

### Cleanup and deletion

Cleanup has two layers, governed by `spec.deletionPolicy` (`Background` default, `Foreground`, or `Orphan`).

**Child OCM CRs** (`Repository`, `Component`, `Resource`) are created in the `OCMDeployment`'s namespace
with the `OCMDeployment` as owner reference and are garbage-collected with it, regardless of
`deletionPolicy` (the policy governs only the deploy objects).

**Generated deploy objects** (`HelmRelease`/`Kustomization`/Argo `Application`) live in the
`OCMDeployment`'s namespace and are applied and pruned via the `applyset` machinery, driven by the
`OCMDeployment`'s finalizer on delete:

* `Background`/`Foreground`: the finalizer prunes them; engine objects cascade-delete their
  workloads where the engine supports it (Kustomization rendered with `prune: true`, Argo rendered
  with the `resources-finalizer.argocd.argoproj.io` finalizer). For Helm the HelmRelease is rendered
  identically regardless of policy; helm-controller's own uninstall-on-delete tears the workload
  down (see the Helm orphan caveat below — Flux has no cascade switch to render).
* `Orphan`: the controller drops its ApplySet `part-of` label and owner reference from each member
  and leaves the objects for another owner to adopt (matching `NamespacedDeployer`'s
  `DeletionPolicy`); the parent's ApplySet annotations are not stripped. Engine objects are rendered
  non-cascading (Kustomize `Orphan` sets `kustomize.toolkit.fluxcd.io/prune: disabled` under
  `spec.commonMetadata.annotations`, no Argo finalizer).

The engine object's *own* deletion behavior (whether removing the
`HelmRelease`/`Kustomization`/`Application` also tears down its workload) is derived from the same
`deletionPolicy`, not a separate field. It is expressible in the rendered object for Kustomize and
Argo but not for Flux `HelmRelease` (see the **Helm `Orphan` caveat** below).

The source (`OCIRepository` or `HelmRepository`) is an `applyset` member like the other deploy
objects, so on `Orphan` it is left in place with them.

A `Namespace` rendered for `namespace.createNamespace` is also an `applyset` member (cluster-scoped
member of a namespaced parent; membership is the ApplySet label, not an owner reference). It is
handled identically for both engines — created and, on a non-`Orphan` `deletionPolicy`, deleted only
when `spec.namespace.pruneNamespace` is true — entirely through the impersonated apply service
account, never the controller's own identity; Argo's native `syncOptions: CreateNamespace` is
deliberately not used, so there is a single SA-owned creator. The namespace delete on prune is an
explicit `Delete` through that service account, not the ApplySet prune sweep (which covers the
namespaced members). Consequently the SA-gone delete fallback — which strips labels with the
controller's own client — releases only the namespaced engine members and leaves any `Namespace`
member untouched; the controller holds no namespace-write RBAC, so an operator must remove the
namespace manually in this case.

`namespace.targetNamespace` selects where the engine places workloads (Flux `HelmRelease`/`Kustomization`
`spec.targetNamespace`; Argo `Application.spec.destination.namespace`), not where the deploy object
lives — that stays in the `OCMDeployment`'s namespace, so there is no cross-namespace owner reference.
A different target *cluster* is selected through the per-engine options: `flux.kubeConfig`
(→ the engine object's `spec.kubeConfig`) and `argo.destination` (→ the cluster selector of `Application.spec.destination`,
which `targetNamespace` completes with `destination.namespace`). The controller still creates and
impersonate-applies the engine object on the local cluster; the engine then performs the remote apply
with its own credential (the referenced kubeconfig, or the cluster Argo CD has registered), so the
controller holds no remote-cluster RBAC of its own.

**Helm `Orphan` caveat**: `Orphan` leaves the engine objects in place, so the workload keeps running
— but it does not protect the workload from a *later* deletion of the retained object. Kustomization
(`prune: disabled`) and Argo (no finalizer) leave the workload on such a deletion; helm-controller,
however, runs a Helm uninstall when a `HelmRelease` is deleted, and `uninstall.deletionPropagation:
orphan` does not prevent it (it only orphans dependents like ReplicaSets/Pods while still deleting the
Deployment, Service, etc.). Whoever adopts a retained `HelmRelease` must suspend it before deleting it
(a suspended `HelmRelease` skips the uninstall).

**Argo `Orphan` caveat**: removing the finalizer does not disable `syncPolicy.automated.prune: true`;
workloads remain only until the next automated sync after the `Application` is deleted.

### OCMDeployment resource

```go
// XValidation: argo unset unless deployer==ArgoCD; flux unset unless deployer==FluxCD; deployer immutable.
type OCMDeploymentSpec struct {
    Source          ComponentSource `json:"source"`                   // required
    DeployResource  OCMResourceRef  `json:"deployResource"`           // required; Helm vs kustomize derived from the OCM resource type
    Deployer        DeployerEngine  `json:"deployer"`                 // required; Enum=FluxCD;ArgoCD; immutable (switching engines = delete+recreate)
    Namespace       *NamespaceOptions `json:"namespace,omitempty"`
    ReleaseName     string          `json:"releaseName,omitempty"`    // defaults to the OCMDeployment name so deployments don't collide; ignored for kustomize
    ApplyServiceAccountName string  `json:"applyServiceAccountName,omitempty"` // SA the controller impersonates to apply; default namespace default SA (fails closed)
    Flux            *FluxOptions    `json:"flux,omitempty"`
    Argo            *ArgoOptions    `json:"argo,omitempty"`
    DeletionPolicy  DeletionPolicy  `json:"deletionPolicy,omitempty"` // Enum=Background;Foreground;Orphan; default Background
    ConfigResources []OCMResourceRef `json:"configResources,omitempty"` // OCM resources providing Helm values (merged) or kustomize patches (passed through)
    Config          *apiextensionsv1.JSON `json:"config,omitempty"`   // inline config merged over ConfigResources (Helm values) or appended after them (kustomize patches); XPreserveUnknownFields
    OCMConfig       []OCMConfiguration `json:"ocmConfig,omitempty"`   // OCM credentials/config, as in all OCM CRs
    Interval        metav1.Duration `json:"interval"`                 // required; fallback cadence only (Owns watches drive reconciles)
    Suspend         bool            `json:"suspend,omitempty"`        // stops reconciling and propagates to engine objects (Flux spec.suspend; Argo syncPolicy.automated.enabled=false)
}

type NamespaceOptions struct {
    TargetNamespace string `json:"targetNamespace,omitempty"` // defaults to the OCMDeployment's namespace
    CreateNamespace *bool  `json:"createNamespace,omitempty"` // default true
    PruneNamespace  bool   `json:"pruneNamespace,omitempty"`  // default false; deletes the whole namespace on non-Orphan delete (opt-in)
}

type ComponentSource struct {
    RepositorySpec  *apiextensionsv1.JSON `json:"repositorySpec"` // required; OCM repo config (XPreserveUnknownFields); http baseUrl for insecure/local
    Component       string `json:"component"`                    // required
    Semver          string `json:"semver"`                       // required; exact or range
    SemverFilter    string `json:"semverFilter,omitempty"`
    DowngradePolicy DowngradePolicy `json:"downgradePolicy,omitempty"` // Enum=Allow;Deny; default Deny (blocks accidental lower resolution)
    Interval        metav1.Duration `json:"interval"`            // required; child Component fetch cadence, independent of spec.interval
}

// Fields inlined (not embedding ResourceID) to avoid inheriting future ResourceID additions.
type OCMResourceRef struct {
    ByReference        ResourceReference  `json:"byReference"`                  // required; set referencePath only for a sub-component resource
    VerificationPolicy VerificationPolicy `json:"verificationPolicy,omitempty"` // Enum=Always;Never; default Always
}

type FluxOptions struct {
    ServiceAccountName string `json:"serviceAccountName,omitempty"` // SA Flux impersonates to sync the workload; distinct from ApplyServiceAccountName; default namespace default SA
    SourceSecretRef *corev1.LocalObjectReference `json:"sourceSecretRef,omitempty"` // → source secretRef (OCIRepository, or HelmRepository for a Helm/v1 source)
    KubeConfig *FluxKubeConfig `json:"kubeConfig,omitempty"`        // → HelmRelease/Kustomization spec.kubeConfig; remote target cluster; referent lives in the OCMDeployment's namespace; unset = local cluster
}

type FluxKubeConfig struct {
    SecretRef    *corev1.LocalObjectReference `json:"secretRef,omitempty"`    // Secret holding a static kubeconfig; mutually exclusive with ConfigMapRef
    ConfigMapRef *corev1.LocalObjectReference `json:"configMapRef,omitempty"` // ConfigMap for a workload-identity kubeconfig (Flux-recommended); mutually exclusive with SecretRef
}

type ArgoOptions struct {
    Project string `json:"project,omitempty"`                       // → Application.spec.project; required in practice (default AppProject rejects non-control-plane namespaces)
    RepoCredentialsSecretRef *corev1.LocalObjectReference `json:"repoCredentialsSecretRef,omitempty"` // Argo-namespace Secret for a Helm/v1 HTTP repo (Argo resolves it out of band)
    Destination *ArgoDestination `json:"destination,omitempty"`     // → Application.spec.destination cluster selector; combines with namespace.targetNamespace (→ destination.namespace); unset = in-cluster (https://kubernetes.default.svc)
}

type ArgoDestination struct {
    Server string `json:"server,omitempty"` // target cluster API server URL; mutually exclusive with Name
    Name   string `json:"name,omitempty"`   // name of a cluster registered with Argo CD; mutually exclusive with Server
}

type OCMDeploymentStatus struct {
    ObservedGeneration int64                     `json:"observedGeneration,omitempty"`
    Conditions         []metav1.Condition        `json:"conditions,omitempty"`
    EffectiveOCMConfig []OCMConfiguration        `json:"effectiveOCMConfig,omitempty"`
    Deployed           []DeployedObjectReference `json:"deployed,omitempty"` // engine objects applied (OCIRepository/HelmRepository/HelmRelease/Kustomization/Application/Namespace); child OCM CRs are owner-GC'd, not listed
    DeployedComponentVersion string            `json:"deployedComponentVersion,omitempty"`
    DeployedResourceDigest   string            `json:"deployedResourceDigest,omitempty"` // anchor for a future re-match-gating optimization
    AppliedLocalizations []AppliedLocalization `json:"appliedLocalizations,omitempty"` // empty = no labelled image matched, not incomplete
}

// AppliedLocalization fields are +required. DigestPinned=false ⇒ tag-only chart, image unverified (warning Event emitted).
type AppliedLocalization struct {
    MatchKey     string `json:"matchKey"`
    Applied      string `json:"applied"`
    DigestPinned bool   `json:"digestPinned"`
}
```

Everything at the top level of the spec is engine-neutral; `deployer` is the discriminator and the
optional `flux`/`argo` blocks are its variants (a validation rule rejects the block that does not
match `deployer`, which is immutable, so the active block is stable for the object's lifetime).
A further engine is additive — a new `deployer` value and a new block — and where engines
genuinely differ (Flux's per-object SA vs Argo's `AppProject`) the difference stays visible in the API
rather than hidden behind a shared field.

### Status conditions

Status is the standard `Ready` condition: `True` once the rendered engine object is healthy (reaching
that point implies every child `Resource` resolved and verified earlier in the same reconcile, since a
not-ready child short-circuits the pipeline), `False` otherwise with a reason and a diagnostic
message. The authoritative reason constants live in `condition_types.go`. Readiness is delegated to
the deploy engine — `Ready=True` reflects the engine's own health verdict (Flux object `Ready`, Argo
`Application` healthy) and the controller does not independently probe the workload's Pods.

A child that never becomes ready (an unresolvable deploy, config, or image `Resource`) is not a
terminal failure: the `OCMDeployment` stays `Ready=False` with the child's reason and requeues on the
controller's rate limit indefinitely — there is no deploy timeout. For `deployer: ArgoCD` there is
one engine-specific diagnostic: when the rendered `Application` carries no status at all, Argo CD has
not reconciled it (most often because apps-in-any-namespace is not configured for its namespace), so
the controller sets `Ready=False` with a message pointing at that prerequisite (see
[Negative Consequences](#negative-consequences)). It is surfaced whenever the Application has no
status and clears as soon as Argo writes any — no timer or persisted timestamp is involved.

### Managed resources

From one `OCMDeployment` the controller creates and owns:

| Object | Built from | Lifecycle |
| --- | --- | --- |
| `Repository` + `Component` | `source.*` | owner-ref → GC with the parent |
| deploy `Resource` | `deployResource` | owner-ref → GC |
| config `Resource` (per `configResources` entry) | `configResources[]` | owner-ref → GC |
| image `Resource` (Helm: per referenced image; kustomize: per labelled image — see [fan-out](#image-resource-fan-out-and-reconcile-cost)) | matched labels | owner-ref → GC; pruned/created as the image set changes |
| deploy objects (Flux/Argo source + release/kustomization/application, optional `Namespace`) | render | `applyset` + finalizer, governed by `deletionPolicy` |

Making the OCM resources first-class CRs (rather than resolving inline) means resolution and
verification happen once, cached and drift-tracked by the existing controllers and visible for
debugging. Child objects are named after the `OCMDeployment` so same-version deployments do not
collide; the controller passes `ocmConfig` through and performs no verification of its own. The
deploy `Resource` is digest-verified by its controller (the chart is checked against the signed
descriptor digest, for HTTP Helm charts as well as OCI), so the controller never inspects a tampered
chart. For OCI the engine is pinned to that same digest, making the inspected and deployed chart
identical; for a `Helm/v1` HTTP chart the engine pulls by reference, so a same-version upstream
republish could still diverge on the engine side — acceptable because a chart published without a
signed digest is an explicit opt-out of integrity.

### Localization

Localization knowledge is split: the signed component records only *which* image to localize (its
coordinates and digest, via the label below), never *where* that image sits in a chart's values. The
values-path binding is resolved at deploy time by inspecting the chart, so signing never depends on
chart structure.

A signed label is attached to each localizable image resource at construction. Presence is the opt-in
marker; the value is the match key. The resource identity is implicit in which resource carries it.

These labels are derived before transfer by `ocm add cv --localize-hints`, which labels every
OCI-access resource (the chart binding is a deploy-time concern, so it cannot know which images a
chart uses) with its coordinates and digest; `--skip-reference-digest-processing` drops the digest,
leaving coordinates only (localization then falls back to tag substitution). Labels authored directly
in the component descriptor are respected — `--localize-hints` skips any resource already carrying one.

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
      digest: sha256:262578...   # present when add cv computed a digest
```

The `matchKey` field is the image's pre-transfer coordinates — the reference still present in the
deploy artifact (chart values, kustomize manifests). After transfer the resource `access` is
rewritten to the new registry location (`transfer/internal/uploader.go`,
`repositoryupload/upload.go`), and OCM deliberately excludes `access` from the signature
(`descriptor/normalisation/json/v4alpha1/normalisation.go`), so the pre-transfer coordinates
survive nowhere else. The signed label is the only channel that preserves them across transfer.
Without `matchKey` there is no key to match against the artifact.

At deploy time the controller matches each label's `matchKey` against the deploy target to decide
which images to localize, and takes the current resolved location (registry, repository, tag, digest)
from the matched image's child `Resource` status.

* **Helm**: fetch the chart (from its OCI or HTTP Helm-repository access) before deploy, load its
  default `values.yaml`, and locate image references deterministically (see
  [Matching (Helm)](#matching-helm)). Where a value matches a label's `matchKey` coordinates, replace
  it in place with the current location. Because this match is computed against `values.yaml` up
  front, a child `Resource` is created only for labelled images actually referenced by the chart; a
  labelled-but-unreferenced image spawns no `Resource` and no verification.
* **Kustomize**: use kustomize's native `images` transformer, a top-level `images:` list that
  rewrites every container image matching a given name across all rendered manifests. One entry
  (`name` = original reference, `newName`/`newTag`/`digest` = current location) localizes the image
  without targeting a manifest path, because kustomize matches by image name. The equivalent
  lower-level form is a JSON6902 patch on the container `image` field; the transformer is preferred
  here precisely because it matches by name instead of a hardcoded path.

#### Matching (Helm)

Matching is exact, not best-effort. The controller walks the loaded values tree and recognizes two
value shapes:

1. a scalar string that parses as an OCI reference (`image: ghcr.io/org/app:1.2.3`), and
2. a map carrying the conventional image sub-keys (`repository`, or `image` as its alias, plus
   optional `registry`, `tag`, `digest`) that reconstruct into a reference.

The walk keys off the value shape, not the field name, so the holder field being called `image` or
anything else does not matter.

Both sides are normalized (default `docker.io` registry, implicit `library/` namespace, canonical
host form) and the **registry+repository** of the reconstructed reference is compared to the label's
`matchKey` registry+repository. The match key is deliberately registry+repository only: the tag or
digest the value carries is not part of the key, because the label `matchKey` and the chart value
originate from the same component build, and a chart that bumps only its image tag must still relocate
to the same repository. Any tag matches, including an empty tag (Helm itself falls back to
`Chart.appVersion` at template-render time; the controller does not substitute appVersion). The
current location's tag/digest are written back on substitution. On a match the value is rewritten
in place: a scalar becomes the full new reference; a map has its `repository`/`registry`/`tag` (and
`digest` when the shape carries one) set to the current location.

A label that matches nothing is skipped, not an error: construction labels every localizable image,
and a given deployment's chart references only some of them, so unused labels are expected. A single
label matching several values localizes all of them (the same image used in several places). The one
error is a genuine conflict: a single value matched by two labels that resolve to different locations.

Integrity comes from the digest, not the label. The resource `digest` is in the signed surface
(normalization excludes `access`/`srcRefs`/unsigned labels, not `digest`), and `ocm add cv` computes
it by default unless `--skip-reference-digest-processing` is set. The digest written on substitution
is the **resolved post-transfer digest from the matched image's child `Resource` status** (that
Resource is digest-verified against the signed descriptor), falling back to the signed label's
`matchKey.digest` only when the status carries none. The emitted override therefore **pins that
digest when the chart's image field accepts a digest, and substitutes registry+repository+tag
otherwise**. The label is the match key that survives transfer; the digest is
the tamper-evidence. Tag-only charts cannot be digest-pinned, so the substituted location rides on the
unsigned (relocated) `access` with no digest to verify it, and Helm pulls by tag at runtime: this is
unverified, not merely TOCTOU-weak. It is not fail-closed (the user chose a tag-only chart); instead
the controller emits a warning Event and sets `appliedLocalizations[].digestPinned: false` for that
image, so the gap is visible in status.

### Image Resource fan-out and reconcile cost

Image localization creates one child `Resource` CR per localized image (`<dep>-img-N`), which the
`Resource` controller verifies (digest processing, default `VerificationPolicy: Always`) and whose
status yields the relocated location. The engines differ in *which* labelled images get a CR:

* **Helm** filters: the controller matches each label's match-key against `values.yaml` (it is the
  authoritative Helm matcher) and creates a `Resource` only for referenced images. Safe because the
  gating match is the same one that performs localization.
* **Kustomize** creates a `Resource` for every labelled `ociImage`, used or not, because matching
  happens by name engine-side at apply time; the controller has no authoritative match to filter on.
  A CR for an unreferenced image is a no-op in the rendered output. This is accepted: for an
  `ociImage` verification is a manifest-digest resolve-and-compare, not a content pull, so broad
  labelling (`--localize-hints` labels every OCI-image resource) costs one metadata resolve per
  labelled image, not a download. Filtering kustomize CRs by a controller-side `kustomize build` was
  rejected — the controller build can diverge from the engine's apply-time build (version, options,
  remote bases), and a successful-but-divergent build would drop an image the engine relocates,
  leaving it unverified.

To keep the fan-out from amplifying reconciles, the owned-`Resource` watch uses a predicate that
enqueues the `OCMDeployment` only on a child's create, delete, or `Ready` transition — not on the
intervening status writes. Without it, each of the N children emits many status writes while
resolving and every write would re-enqueue the parent, whose reconcile re-examines all N children —
roughly O(N²) work; the predicate collapses that to O(N) meaningful wakeups.

### Configuration and merge order

#### Helm

For Helm, configuration and localization are merged into the single `values`/`valuesObject` field
in precedence order (maps merge recursively by key, arrays replace, image blocks are atomic — see
rules below):

```text
chart defaults            (applied by Helm)
  + localization overrides (beats chart defaults, its only job)
  + config resources       (explicit, beats localization)
  + inline config          (most specific, wins)
```

Merge rules:

* Maps deep-merge by key; later layers win on overlapping leaves.
* An image block (the subtree a localization override wrote) is treated **atomically**: config
  either leaves it untouched or replaces the whole block. A deliberate whole-block image override in
  config therefore wins (the escape hatch when localization matched wrong). A **partial** override
  (config sets only `tag` while localization set `repository`+`tag`+`digest`) is **rejected** with
  `Ready=False` (`reason: UnsupportedConfiguration`), not silently dropped: it cannot be applied
  coherently — against a digest-pinned block the digest wins at runtime, so the new `tag` would be
  silently ignored, and in every case the whole-block override is the supported way to change a
  localized image. A whole-block override from inline `config` can replace a digest-pinned image and
  bypass the signed-digest integrity below; that is accepted as an explicit, operator-authored
  override, not a silent one.
* Arrays are replaced, not merged.

#### Kustomize

For kustomize there is no `values` field, so localization and configuration use two distinct,
independent mechanisms.

* **Localization** uses the `images` transformer, not patches (see [Localization](#localization)).
* **Configuration** uses JSON6902 / strategic-merge patches against the rendered manifests, rendered
  into the sibling field Flux `Kustomization.spec.patches` / Argo `source.kustomize.patches` (both
  engines support it; see
  [`examples/deployment/flux-kustomize/`](../../bindings/go/kubernetes/controller/examples/deployment/flux-kustomize/)
  and [`examples/deployment/argo-kustomize/`](../../bindings/go/kubernetes/controller/examples/deployment/argo-kustomize/)).
  `config` and `configResources` each carry a top-level `patches` list (`{patch: <string>, target:
  {…}}` entries, JSON6902 or strategic-merge) passed through verbatim into that field. Because `config`
  is typed `+kubebuilder:validation:Type=object`, inline kustomize config must be an object —
  `{"patches": [ … ]}`, never a bare top-level array.

Patch ordering: entries from `configResources` come first (in spec order), then entries from inline
`config` last — later patches apply after earlier ones. A config source that does not contain a valid
top-level `patches` list is rejected with `Ready=False` (`reason: UnsupportedConfiguration`). Other
kustomize fields are not modeled: `replicas`, `commonMetadata`/`commonLabels` and `postBuild` do not
exist on both engines, and the first two are expressible as patches.

### Deployer engine and artifact matrix

| Source \ Engine | FluxCD | ArgoCD |
| --- | --- | --- |
| Helm (OCI) | `OCIRepository` + `HelmRelease.chartRef` + `.values` | `Application.source` (`oci://` repoURL) + `helm.valuesObject` |
| Helm (HTTP repository, `Helm/v1`) | `HelmRepository` + `HelmRelease.chart.spec.sourceRef` + `.values` | `Application.source` (https repoURL, `chart`, `targetRevision`) + `helm.valuesObject` |
| Kustomize | `OCIRepository` + `Kustomization` (`images`; `patches` from config/configResources) | `Application.source.kustomize` (`images`; `patches` from config/configResources) |

A `Helm/v1` deploy resource (a chart in a classic HTTP/HTTPS Helm repository) is a valid OCM
component shape, typical of a by-reference transfer that leaves the access at its upstream location.
It is rendered as a native HTTP Helm source (table row above) so the engine pulls the chart at its
own identity. For localization the controller pulls the chart read-only (via the Helm resource
download plugin) to read `values.yaml`, exactly as for an OCI chart; only the rendered source differs.
Credentials: Flux reuses `flux.sourceSecretRef` (→ `HelmRepository.spec.secretRef`); Argo resolves
them from a Secret in its own namespace named by `argo.repoCredentialsSecretRef`.

Localization input is identical across every source/engine combination; only the output field
differs. Apply and prune of the generated deploy objects use the `applyset` utility directly (as the
`NamespacedDeployer` does), not the deployer CRs themselves.

The kustomize `images` field is shaped per engine because the CRDs differ: Flux's `Kustomization`
types it as structured objects (`[{name, newName, newTag, digest}]`), Argo's `Application` types it
as kustomize shorthand strings (`"name=newName:newTag@digest"`). Both encode the same localization;
only the serialization the engine's API accepts differs.

Argo `Application`s are rendered with `syncPolicy: {automated: {prune: true, selfHeal: true}}`
so they sync automatically without operator intervention; without this block an `Application` does
not sync on its own.
`spec.suspend` propagates to both engines: Flux objects receive `spec.suspend: true`, and the Argo
`Application` is rendered with `syncPolicy.automated.enabled: false` (which skips automated sync while
keeping `prune`/`selfHeal` configured, restored intact on resume — supported since Argo CD 3.1.0).
Flux and Argo engine-specific fields beyond those modelled (`timeout`, remediation, `certSecretRef`,
`provider`, `retry`, `syncOptions`, `ignoreDifferences`) are not rendered; engine defaults apply and
the fields can be added to the per-engine option blocks when real use cases arise.

### Engine API versions

The controller renders fixed, stable engine `apiVersion`s (no spec version field): Flux
`source.toolkit.fluxcd.io/v1` (`OCIRepository`, `HelmRepository`), `helm.toolkit.fluxcd.io/v2`
(`HelmRelease`), `kustomize.toolkit.fluxcd.io/v1` (`Kustomization`) — floor **Flux 2.6.0**; Argo
`argoproj.io/v1alpha1` (`Application`) — floor **Argo CD 3.1.0** (native OCI digest in
`targetRevision`, needed to pin the chart). Plain-HTTP OCI registries additionally need Argo CD 3.5
(`insecureOCIForceHttp`, set operator-side, not an `OCMDeployment` field).

Objects are rendered as `unstructured.Unstructured` with the `apiVersion`/`kind` constants in the
render package, so the controller imports no Flux/Argo Go module and an engine release does not force
a rebuild; the tradeoff is that field names are validated only at apply time (the CRD schema), not by
the Go compiler. The controller does not enforce the version floors — a missing CRD surfaces as the
only explicit `Ready=False`.

## Pros and Cons of the Options

### Positive Consequences

* One applied `OCMDeployment` instead of an RGD plus bootstrap objects.
* Localization is automatic; integrity is anchored on the signed digest when the chart allows it.
* Configuration can be shipped with the component (as `configResources`) and layered per
  deployment via inline `config`.
* No new template language is introduced.

### Negative Consequences

* Helm localization depends on the chart exposing images as literal values in a recognized shape
  (scalar OCI reference, or a map with `registry`/`repository`/`tag`/`digest` keys); images present
  only in templates are out of scope. Kustomize avoids the search via its native `images` transformer.
  See [Matching (Helm)](#matching-helm).
* Tag-only charts cannot be digest-pinned, so their localized image is unverified (warned, not
  blocked; see [Localization](#localization)).
* Helm orphan caveat: see [Cleanup and deletion](#cleanup-and-deletion). The escape hatch is to adopt
  and suspend the retained `HelmRelease` before deleting it.
* `ocmConfig` credentials (which may be a `.dockerconfigjson` Secret) authenticate the controller's
  own pull from the OCM registry; they are not propagated to the engine's pull of the relocated chart
  or the workload's image pulls. Those are configured separately: `flux.sourceSecretRef` (→ the Flux
  source `secretRef`) or an Argo CD repository Secret for the chart, and Pod `imagePullSecrets` (set
  via `config`/`patches`) for the relocated images.
* `ArgoCD` assumes a prepared Argo CD: apps-in-any-namespace must be enabled for the `OCMDeployment`'s
  namespace (`application.namespaces` in `argocd-cmd-params-cm`) and that namespace listed in the
  `AppProject`'s `sourceNamespaces` (Argo CD enforces both in `IsAppNamespacePermitted`; the `default`
  project ships an empty `sourceNamespaces` and must not be populated). An unset `argo.project`
  therefore fails unless a dedicated project is configured. The controller applies the `Application`
  and, while it carries no status (Argo CD has not reconciled it), sets `Ready=False` pointing at
  these prerequisites.
* By default Argo CD syncs with its own control-plane SA (cluster-admin-equivalent); scoping it
  requires Argo CD 3.5 operator-side steps (see [Apply identity and RBAC](#apply-identity-and-rbac)).

## Conclusion

`OCMDeployment` replaces the three-to-five-resource v1 orchestration pattern with a single typed CR
whose controller owns all child objects. The core design choices — signed-label localization derived
at construction, engine-neutral spec with per-engine option blocks, impersonate-apply for deploy
isolation — follow directly from the decision drivers and are accepted with their stated tradeoffs
(tag-only image integrity gap, Helm orphan caveat).

Deliberate non-goals for this revision — accepted scope limits, not rejected designs:

* only Flux and Argo are supported — the controller is not a pluggable, fully engine-agnostic deployer
  (the spec is engine-neutral, but the render/health paths are Flux/Argo-specific);
* images declared in Helm subchart (dependency) values are not localized.
