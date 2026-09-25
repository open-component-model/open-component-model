# Use Single Layer OCI Artifacts for (intermediate) blobs

* Status: proposed
* Deciders: @frewilhelm @ikhandamirov
* Approvers:

Technical Story: <https://github.com/open-component-model/ocm-project/issues/333>

## Context and Problem Statement

The controllers in this repository create artifacts/blobs that are used by one another. For example, the
component-controller creates an artifact containing the component descriptors from the specified component version.
Finally, the resource controller, or if specified the configuration controller, creates a blob as an artifact that holds
the resource that is consumed by the deployers.

Initially, it was planned to use a Custom Resource `artifact` type to represent these artifacts.
This `artifact` type was [defined][artifact-definition] to point to a URL and holds a "human-readable" identifier
`Revision` of a blob stored in a http-server inside the controller.

The `artifact` idea was part of a bigger [RFC][fluxcd-rfc] for `FluxCD`. Unfortunately, the change would be difficult
to communicate and potentially prompt security audits on FluxCDs customer side. Thus, the proposal was not acceptable
in the given format due to the differences on the watch on the `artifact` resource. This was tantamount to a rejection.

Therefore, the original purpose of that Custom Resource `artifact` is not present anymore. Additionally, the team
decided to not use a plain http-server but an internal OCI registry to store and publish its blobs that are produced by
the OCM controllers as single layer OCI artifacts.

Arguments:

* In comparison to the current plain http-server that requires an implementation of access, GC, ... the usage of an OCI
  registry
  could simplify the implementation (e.g. deleting dangling blobs after a manifest was deleted)
* Stop support at the level of the distribution spec of OCI
* We will need an abstraction that handles OCI registries anyway to convert resources into a flux consumable format
(= single layer oci artifact)

The following discussion concerns two major topics:

* How to store and reference the single layer OCI artifacts.
* How to setup the internal OCI registry and which one to use.

## Decision Drivers

* Reduce maintenance effort
* Fit into our use-cases (especially with FluxCD)

## Artifact (How to store and reference the single layer OCI artifacts)

An artifact in the current context describes a resource that holds an identity to a blob and a pointer where to find
the blob (currently a URL). In that sense, a producer can create an artifact and store this information and a consumer
can search for artifacts with the specific identity to find out its location.

In the current implementation the artifact is defined in the [openfluxcd/artifact repository][artifact-definition].

The ocm-controller `v1` implementation defined a `snapshot` type that serves similar purposes.
Its definition can be found in [open-component-model/ocm-controller][snapshot-definition].

### Comparison `artifact` vs `snapshot`

To enable the following option discussion, the fields of the CRs `artifact` and `snapshot` are compared:

#### Snapshot

From [ocm-controller v1 Architecture][ocm-controller-v1-architecture]:
_snapshots are immutable, Flux-compatible, single layer OCI images containing a single OCM resource.
Snapshots are stored in an in-cluster registry and in addition to making component resources accessible for
transformation, they also can be used as a caching mechanism to reduce unnecessary calls to the source OCM registry._

[`SnapshotSpec`][snapshot-spec]

* Identity: OCM Identity (map[string]string) (Created by [constructIdentity()][snapshot-create-identity])
* Digest: OCI Layer Digest (Based on [go-containerregistry OCI implementation][go-containerregistry-digest])
* Tag: The version (e.g. `latest`, `v1.0.0`, ..., see [reference][snapshot-version-ref]
* (Suspend)

[`SnapshotStatus`][snapshot-status]

* (Conditions)
* LastReconciledDigest: determine, if reconciliation is necessary. Marks the last SUCCESSFULLY reconciled digest.
* LastReconciledTag: determine, if reconciliation is necessary. Marks the last SUCCESSFULLY reconciled tag.
* RepositoryURL: Concrete URL pointing to the local registry including the service name
* (ObservedGeneration)

#### Artifact

[`ArtifactSpec`][artifact-spec]

* URL: HTTP address of the artifact as exposed by the controller managing the source
* Revision: "Human-readable" identifier traceable in the origin source system (commit SHA, tag, version, ...)
* Digest: Digest of the file that is stored (algo:checksum)
    * Used to verify the artifact (see [artifact-digest-verify-ref][artifact-digest-verify-ref])
* LastUpdateTime: Timestamp of the last update of the artifact
* Size: Number of bytes in the file (decide beforehand on how to download the files)
* Metadata: Holds upstream information, e.g. OCI annotations (as map[string]string)

[`ArtifactStatus`][artifact-status]

* No fields

### Considered Options

* Option 1: Omit the `artifact`/`snapshot` concept
* Option 2: Use the `snapshot` implementation
* Option 3: Use the `artifact` implementation
* Option 4: Use `OCIRepository` implementation from `FluxCD`
* Option 5: Create a new custom resource

### Decision Outcome

Chosen option: "Option 1: Omit the `artifact`/`snapshot` concept", because the implementation gets simpler by not
needing an additional custom resource and reconciler. All the required information ("Where to find the blob") can be
stored in the status of the source resource (`component`, `resource`, `configuredResource`).

#### Positive Consequences

* Simpler implementation.
* No intermediate Custom Resource necessary.

#### Negative Consequences

* The final OCI artifact that should be used for the deployment needs to be consumable by a deployment-tool, e.g.
  FluxCDs `source-controller`.
  Thus, some kind of consumable Kubernetes resource must be created, e.g. FluxCDs `OCIRepository`.
  More details can be retrieved from the respective Deployment ADR.

### Pros and Cons of the Options

#### Option 1: Omit the `artifact`/`snapshot` concept

Instead of using an intermediate Custom Resource as `artifact` or `snapshot`, one could update the status of the source
resource that is creating the blob could point to the location of that blob itself.

Pros:

* No additional custom resource and reconciler needed.

Cons:

* Loss of extensibility of the architecture provided by a common interface.

#### Option 2: Use the `snapshot` implementation

Pros:

* Already implemented (and probably tested).
* Implemented for an OCI registry

Cons:

* Requires a transformer to make the artifacts consumable by FluxCDs Helm- and Kustomize-Controller. E.g. by using
FluxCDs `source-controller` and its CR `OCIRepository`.
* Implemented in `open-component-model/ocm-controller` which will be archived, when the `ocm-controller` v2 go
productive. Thus, the `snapshot` implementation must be copied in this repository.

#### Option 3: Use the `artifact` implementation

Pros:

* Already implemented (and a bit tested)
* Rather easy and simple

Cons:

* Implemented for a plain http-server and not for OCI registry (check
  [storage implementation][controller-manager-storage]). Thus, missing dedicated control-loop.
* The current setup (`OpenFluxCD`) requires the deployment of the customized FluxCD `helm-` and `kustomize-controller`.
This wouldn't be required necessarily, but some form of transformation of the `artifact` resource to a consumable
resource is necessary, e.g. by a transformer as in Option 2.
* Maintenance of the storage server (which is copied and adjusted from flux).

Basically rejected because we could only use the `Artifact` type definition and not the implementation for the storage.

#### Option 4: Use `OCIRepository` implementation from `FluxCD`

See [definition][oci-repository-type]. The type is part of the FluxCDs `source-controller`, which also
provides a control-loop for that resource.

Pros:

* No transformer needed for `FluxCD`s consumers Helm- and Kustomize-Controller
* Control-loop for `OCIRepository` is already implemented
* `OCIRepository` is an integration point with Flux and Argo

Cons:

* Integrating FluxCDs `source-controller` would be a hard dependency on that repository. It would be mandatory
to deploy the `source-controller`
* It is not possible to start the `source-controller` and only watch the `OCIRepository` type. It would
start all other control-loops for `kustomize`, `helm`, `git`, and more objects. This seems a bit of an
overkill.
* Using the `OCIRepository` control-loop would basically "clone" every blob from the OCI registry in FluxCD
  local storage (plain http server).

#### Option 5: Create a new custom resource

Pros:

* Greenfield approach.
* Orientation on `snapshot` and `artifact` ease the implementation.

Cons:

* Offers no benefit over the existing implementation.

Creating a new custom resource seems like an overkill, considering that the `snapshot` implementation covers a lot of
our use-cases. Thus, it seems more reasonable to go with the `snapshot` implementation and adjust/refactor that.

## (Internal) OCI Registry

This in-cluster HTTPS-based registry is used by the OCM controllers to store resources locally. It should never be accessible from outside, thus it is transparent to the users of the OCM controller. At the same time the registry is accessible for Flux, running in the same cluster.

### Considered Options

* Option 1: Let the user provide a registry that is OCI compliant
* Option 2: Deploy an OCI image registry with our controllers
  * Option 2.1: Use implementation from ocm-controllers v1
  * Option 2.2: Use [`zot`](https://github.com/project-zot/zot)

### Decision Outcome

Chosen option:

* Option 2.2, i.e. the decision is to use `zot` as the in-cluster OCI registry for OCM controllers
* Once there is an installer for OCM controller, it should provide the users with a possibility to configure an own
  registry instead of embedded `zot`, either an in-cluster or an external one

To select the registry, no comprehensive benchmarking tests have been performed. Zot is vendor-neutral and fully supports OCI standard. The decision is based on the impression that `zot` is meanwhile being more actively maintained and will incorporate innovation faster. The registry comes with an [extensive feature set](https://zotregistry.dev/v2.1.2/general/features/), sufficient for the OCM controllers use case. The first tests have shown that OCM controllers are able to work with `zot`.

### Pros and Cons of the Options

### Option 1: Let the user provide a registry that is OCI compliant

Pros:

* Operating a user-provided registry is not in our responsibility
* Users can customize their OCI registry like they want

Cons:

* We develop and test the OCM Toolset in environments where `zot` registry is used. We assume that it'll work with any
  other OCI-compliant registry, but other registries are not tested (yet).
* Giving a possibility to the user to provide/configure an own registry does not eliminate the need to provide a default
  registry (option 2), especially to those users who do not want to customize an own registry.

#### Option 2: Deploy an OCI image registry with our controllers

Pros:

* Simplifies deployment choices and stability guarantees for us.

##### Option 2.1: Use implementation from ocm-controllers v1 ([distribution registry](https://github.com/distribution/distribution))

Pros:

* Faster implementation time, as deployment can be copied from v1 implementation
* Mature technology (almost legacy)

Cons:

* Seldom releases (latest stable from October 2, 2023)

##### Option 2.2: Use [`zot`](https://github.com/project-zot/zot)

Pros:

* Supports OCI standard, i.e. does not depend on Docker image format
* Newer technology, focusing on embedding into other products, inline garbage collection and storage deduplication
* Nice documentation
* FluxCD team mentioned (verbally) that they want to use a `zot` OCI registry in the future (though no 100% guarantee or
  any evidence that they started working on this so far)
* Being actively maintained (several stable releases per year)
* Vendor neutrality in our distribution that is backed by a project incorporated in a large foundation. Both projects
  are part of CNCF, but docker registry is still mainly maintained by folks at docker

Cons:

* Potentially longer implementation time, as it involves learing how to deploy, configure and operate a new registry
* To support Docker images, the registry must be run in compatibility mode, though our assumption is that our
  stakeholders will work with standard OCI in most cases

# Links

* Epic [#75](https://github.com/open-component-model/ocm-k8s-toolkit/issues/75)
* Issue [#90](https://github.com/open-component-model/ocm-k8s-toolkit/issues/90)

[artifact-definition]: https://github.com/openfluxcd/artifact/blob/d9db932260eb5f847737bcae3589b653398780ae/api/v1alpha1/artifact_types.go#L30
[fluxcd-rfc]: https://github.com/fluxcd/flux2/discussions/5058
[snapshot-definition]: https://github.com/open-component-model/ocm-controller/blob/8588071a05532abd28916931963f88b16622e44d/api/v1alpha1/snapshot_types.go#L22
[ocm-controller-v1-architecture]: https://github.com/open-component-model/ocm-controller/blob/8588071a05532abd28916931963f88b16622e44d/docs/architecture.md

[snapshot-spec]: https://github.com/open-component-model/ocm-controller/blob/8588071a05532abd28916931963f88b16622e44d/api/v1alpha1/snapshot_types.go#L22

[snapshot-status]: https://github.com/open-component-model/ocm-controller/blob/8588071a05532abd28916931963f88b16622e44d/api/v1alpha1/snapshot_types.go#L35
[artifact-spec]: https://github.com/openfluxcd/artifact/blob/d9db932260eb5f847737bcae3589b653398780ae/api/v1alpha1/artifact_types.go#L30
[artifact-status]: https://github.com/openfluxcd/artifact/blob/d9db932260eb5f847737bcae3589b653398780ae/api/v1alpha1/artifact_types.go#L62
[go-containerregistry-digest]: https://github.com/google/go-containerregistry/blob/6bce25ecf0297c1aa9072bc665b5cf58d53e1c54/pkg/v1/manifest.go#L47
[snapshot-version-ref]: https://github.com/open-component-model/ocm-controller/blob/8588071a05532abd28916931963f88b16622e44d/controllers/resource_controller.go#L212
[snapshot-create-identity]: https://github.com/open-component-model/ocm-controller/blob/8588071a05532abd28916931963f88b16622e44d/controllers/resource_controller.go#L287
[artifact-digest-verify-ref]: https://github.com/openfluxcd/controller-manager/blob/d83030b764ab4f143d4b9a815227ad3cdfd9433f/storage/storage.go#L478
[oci-repository-type]: https://github.com/fluxcd/source-controller/blob/529eee0ed1afc6063acd9750aa598d90ae3399ed/api/v1beta2/ocirepository_types.go#L296
[controller-manager-storage]: https://github.com/openfluxcd/controller-manager/blob/d83030b764ab4f143d4b9a815227ad3cdfd9433f/storage/storage.go

