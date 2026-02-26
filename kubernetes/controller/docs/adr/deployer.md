# Deployment ADR

* Status: proposed
* Deciders: @frewilhelm @Skarlso @fabianburth
* Approvers: @jakobmoellerdev

## Context and Problem Statement

The purpose of the ocm-controllers is to provide a way to deploy resources from an OCM component version. As discussed
in the [artifacts ADR](./artifacts.md#negative-consequences) this requires some kind of deployer that consumes the
given resource and uses it to create some kind of deployment.

In the current scope the deployer must be able to deploy a resource, which can be a Helm chart, a Kustomization, or a
plain Kubernetes resource. It will be deployed to a Kubernetes cluster using FluxCD. The resource must be provided as
OCI artifact that FluxCDs `OCIRepository` can consume.

The resource provides a status that holds a reference to an OCI artifact and its manifest. The deployer must be
able to get the OCI artifact reference through a reference to that resource by accessing the status of the resource.

The deployer must make it easy for the user to deploy the resources from the OCM component version. It is easy if the
structure of the deployer is simple and easy to understand, e.g. not requiring a lot of different resources for a
deployment (like `Repository`, `Component`, `Resource`, `OCIRepository`, `HelmRelease`, `Kustomization`, ...).

However, the deployer must also be flexible enough to deploy different kinds of resources and reference several
resources of an OCM component version. It must be possible to configure these resources and dynamically configure
the resource that is deployed, while deploying it. For example, injecting cluster-names or substituting values in the
to-be-deployed resources.

Essentially, the requirements can be breakdown to:

* We need to dynamically create a deployer specific custom resource, e.g. FluxCDs `OCIRepository`, that is filled with
  information provided by the status of a Kubernetes resource OCM `Resource`.
* Simplify the deployment of the building block custom resources (`Repository`, `Component`, `Resource`, ...)

## Decision Drivers

* Simplicity: The deployer should be easy to understand and use. It should not require a lot of different resources to
  be created.
* Flexibility: The deployer should be flexible enough to deploy different kinds of resources and reference several
  resources of an OCM component version. It should be possible to configure these resources and dynamically configure
  the resource that is deployed, while deploying it.
* Maintainability: The deployer should be easy to maintain and extend. It should not require a lot of different
  resources to be created.

## Considered Options

* [Kro](#kro): Use Kro's `ResourceGraphDefinition` to orchestrate all required resources (from OCM Kubernetes-resources
  to the deployment resources).
* [FluxDeployer](#fluxdeployer): Create a CRD `FluxDeployer` that points to the consumable resource and its reconciler
  creates FluxCDs `OCIRepository` and `HelmRelease`/`Kustomization` to create the deployment based on the resource.
  (This option would be closest to the `ocm-controllers` v1 implementation.)
* [Deployer](#deployer): Create a CRD `Deployment` that is a wrapper for all resources that are required for the deployment.

## Decision Outcome

Chosen option: [Kro](#kro), because

* the `ResourceGraphDefinition` is an intuitive way to orchestrate the deployment of resources and map dependencies
  between them.
* the `ResourceGraphDefinition` can be packed into the OCM component version itself.
* using a `ResourceGraphDefinition` with a deployment-tool that can replace values in the deployments itself, e.g.
  FluxCDs `HelmRelease.spec.values` or `Kustomization.spec.patches`, we can omit the localization and configuration
  operators. If we do not need to localize or configure the resources, we do not need to store them in an internal
  storage, which is why we can omit the internal storage as well.

### Positive Consequences

* Maintainability is improved a lot, as we can omit the localization and configuration controllers as well as the
  internal storage.
  * Users do not have to maintain an OCI storage in their production cluster.
* Only one additional operator is required to deploy the `ResourceGraphDefinition` as Kro will take care of the
  resource orchestration as defined in the `ResourceGraphDefinition`.

### Negative Consequences

* Kro is a third-party open-source project that we do not maintain. Thus, we have to rely on the maintainers of Kro to
  keep it up to date and fix bugs. However, the Kro maintainers are open for contributions and already accepted some
  issues and contributions that we reported.
* When the `ResourceGraphDefinition` is part of the OCM component version, the developers themselves must know and be
  aware of the deployment-technology that is used to deploy the resources.

## Pros and Cons of the Options

### Kro

> [!IMPORTANT]
> This approach requires a basic understanding of [kro][kro-github]! Please read the [documentation][kro-doc] before
> proceeding.

Using Kro, developers or operators can define a `ResourceGraphDefinition` with deployment instructions which
orchestrates all required resources for the deployment of a resource from an OCM component version. The deployment can
be very flexible by using CEL expression to configure the resources based on other resources or values from the
instance.

#### A simple use case

A simple use case is an OCM component version containing a Helm chart and using a `ResourceGraphDefinition` to create
the deployment.

![ocm-controller-deployer](../assets/ocm-controller-deployer.svg)

The flowchart shows an OCM repository that contains an OCM component version. This OCM component version holds a Helm
chart. By creating a `ResourceGraphDefinition` with the respective resources referring each other accordingly, the
deployment of that Helm chart can be orchestrated by creating an instance of the resulting CRD `Simple`.

The manifests for such a use case could look like this:

`component-constructor.yaml`

```yaml
components:
  - name: ocm.software/ocm-k8s-toolkit/helm-simple
    version: "1.0.0"
    provider:
      name: ocm.software
    resources:
      # This helm resource contains additional deployment instructions for the application itself
      - name: helm-resource
        type: helmChart
        version: "1.0.0"
        access:
           type: ociArtifact
           imageReference: ghcr.io/stefanprodan/charts/podinfo:6.9.1
```

`resource-graph-definition.yaml`

```yaml
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: helm-simple-rgd
spec:
  schema:
    apiVersion: v1alpha1
    # CRD that gets created
    kind: Simple
    # Values that can be configured using Kro instance (configuration) (= passed through the instance)
    spec:
      releaseName: string | default="helm-simple"
  resources:
    - id: repository
      template:
        apiVersion: delivery.ocm.software/v1alpha1
        kind: Repository
        metadata:
          name: "helm-simple-repository"
        spec:
          repositorySpec:
            baseUrl: ghcr.io/<your-org>
            type: OCIRegistry
          interval: 10m
    - id: component
      template:
        apiVersion: delivery.ocm.software/v1alpha1
        kind: Component
        metadata:
          name: "helm-simple-component"
        spec:
          component: ocm.software/ocm-k8s-toolkit/helm-simple
          repositoryRef:
            name: "${repository.metadata.name}"
          semver: 1.0.0
          interval: 10m
    - id: resourceChart
      template:
        apiVersion: delivery.ocm.software/v1alpha1
        kind: Resource
        metadata:
          name: "helm-simple-resource-chart"
        spec:
          componentRef:
            name: ${component.metadata.name}
          resource:
            byReference:
              resource:
                name: helm-resource
          interval: 10m
    - id: ocirepository
      template:
        apiVersion: source.toolkit.fluxcd.io/v1
        kind: OCIRepository
        metadata:
          name: "helm-simple-ocirepository"
        spec:
          interval: 1m0s
          layerSelector:
            mediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip"
            operation: copy
          # Use values from the resource "resourceChart"
          # A resource reconciled by the resource-controller will provide a SourceReference in its status (if possible)
          #    type SourceReference struct {
          #      Registry   string `json:"registry"`
          #      Repository string `json:"repository"`
          #      Reference  string `json:"reference"`
          #    }
          url: oci://${resourceChart.status.reference.registry}/${resourceChart.status.reference.repository}
          ref:
            tag: ${resourceChart.status.reference.reference}
    - id: helmrelease
      template:
        apiVersion: helm.toolkit.fluxcd.io/v2
        kind: HelmRelease
        metadata:
          name: "helm-simple-helm-release"
        spec:
          # Configuration (passed through Kro instance, respectively, the custom resource "AdrInstance")
          releaseName: ${schema.spec.releaseName}
          interval: 1m
          timeout: 5m
          chartRef:
            kind: OCIRepository
            name: ${ocirepository.metadata.name}
            namespace: default
```

`instance.yaml`

```yaml
# The instance of the CRD created by the ResourceGraphDefinition
apiVersion: kro.run/v1alpha1
kind: Simple
metadata:
  name: helm-simple-instance
spec:
  # Pass values for configuration
  releaseName: "helm-simple-instance"
```

#### A more complex use case with bootstrapping

It is possible to ship the `ResourceGraphDefinition` with the OCM component version itself. This, however, requires some
kind of bootstrapping as the `ResourceGraphDefinition` must be sourced from the OCM component version and applied to the
cluster.

To bootstrap a `ResourceGraphDefinition` an Kubernetes operator is required, e.g. `OCMDeployer`, that takes a Kubernetes
custom resource (OCM) `Resource`, extracts the `ResourceGraphDefinition` from the OCM component version, and applies it
to the cluster.

The developer can define any localisation or configuration directive in the `ResourceGraphDefinition`. The operator only
has to deploy the bootstrap and an instance of the CRD that is created by the `ResourceGraphDefinition` as well as
passing values to its scheme if necessary.

![ocm-controller-deployer-bootstrap](../assets/ocm-controller-deployer-bootstrap.svg)

The flowchart shows a git repository containing the application source code for the image, the according Helm chart,
the component constructor, and a `ResourceGraphDefinition`. In the example, all these components are stored in an OCM
component version and transferred to an OCM repository.

To deploy the application into a Kubernetes cluster, it is required to bootstrap the `ResourceGraphDefinition` by

* deploying an `Repository` that points to the OCM repository in which the OCM component version is stored,
* deploying a `Component` that points to the OCM component version in that OCM repository,
* deploying a `Resource` that points to the `ResourceGraphDefinition` in the component version, and
* deploying an `OCMDeployer` that points to the `Resource` and applies the `ResourceGraphDefinition` to the cluster.

The manifests for that bootstrap cannot be shipped with the OCM component version (as this would require another
bootstrap). Accordingly, the bootstrap manifests must be provided separately and the provider must know which OCM
component version and resources to use.

As one can see, the `ResourceGraphDefinition` in the OCM component version contains the image of the application itself.
The reason for this, is to localise the image:

When an OCM resource get transferred to an OCM repository, the access of the resource is adjusted to the new location.
This access reference can be stored in the status of that `resource` and used to localise the image using FluxCDs
`HelmRelease.spec.values` field. As a result, the image-reference in the Helm chart now points to the new location of
that resource.
(The access reference that is provided by the `Resource` is in `json`, which, currently, cannot be resolved by Kros
`ResourceGraphDefinition`. That is why we currently use a `resource.status.sourceReference` field instead, in which we
parse the access references. However, the Kro maintainers already mentioned that they would welcome such a feature.)

The following manifests show an example of such a setup:

`component-constructor.yaml`

```yaml
components:
  - name: ocm.software/ocm-k8s-toolkit/helm-bootstrap
    version: "1.0.0"
    provider:
      name: ocm.software
    resources:
      - name: helm-resource
        type: helmChart
        version: "1.0.0"
        access:
           type: ociArtifact
           imageReference: ghcr.io/stefanprodan/charts/podinfo:6.9.1
      # This image resource contains the application image (can be used for localisation while deploying)
      - name: image-resource
        type: ociArtifact
        version: "1.0.0"
        access:
          type: ociArtifact
          imageReference: ghcr.io/stefanprodan/podinfo:6.9.1
      # This resource contains the kro ResourceGraphDefinition
      - name: kro-rgd
        type: blob
        version: "1.0.0"
        input:
          type: file
          path: ./resource-graph-definition.yaml
```

`resource-graph-definition.yaml`

```yaml
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: bootstrap-rgd
spec:
  schema:
    apiVersion: v1alpha1
    kind: Bootstrap
    spec:
      releaseName: string | default="bootstrap-release"
      message: string | default="hello world"
  resources:
    - id: resourceChart
      template:
        apiVersion: delivery.ocm.software/v1alpha1
        kind: Resource
        metadata:
          name: helm-chart
        spec:
          componentRef:
            name: component-name
          resource:
            byReference:
              resource:
                name: helm-resource
          interval: 10m
    # Kro resource that is used to do localisation
    - id: resourceImage
      template:
        apiVersion: delivery.ocm.software/v1alpha1
        kind: Resource
        metadata:
          name: image
        spec:
          componentRef:
            name: component-name
          resource:
            byReference:
              resource:
                name: image-resource
          interval: 10m
    # Any deployer can be used. In this case we are using FluxCD HelmRelease that references FluxCD OCIRepository
    - id: ocirepository
      template:
        apiVersion: source.toolkit.fluxcd.io/v1
        kind: OCIRepository
        metadata:
          name: oci-repository
        spec:
          interval: 1m0s
          layerSelector:
            mediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip"
            operation: copy
          url: oci://${resourceChart.status.reference.registry}/${resourceChart.status.reference.repository}
          ref:
            tag: ${resourceChart.status.reference.reference}
    - id: helmrelease
      template:
        apiVersion: helm.toolkit.fluxcd.io/v2
        kind: HelmRelease
        metadata:
          name: helm-release
        spec:
          releaseName: ${schema.spec.releaseName}
          interval: 1m
          timeout: 5m
          chartRef:
            kind: OCIRepository
            name: ${ocirepository.metadata.name}
            namespace: default
          values:
            # Localisation (image location is adjusted to its location through OCM transfer)
            image:
              repository: ${resourceImage.status.reference.registry}/${resourceImage.status.reference.repository}
              tag: ${resourceImage.status.reference.reference}
            ui:
              message: ${schema.spec.message}
```

`bootstrap.yaml`

```yaml
# Repository contains information about the location where the component version is stored
apiVersion: delivery.ocm.software/v1alpha1
kind: Repository
metadata:
  name: repository-name
spec:
  repositorySpec:
    baseUrl: ghcr.io/<your-org>
    type: OCIRegistry
  interval: 10m
---
# Component contains information about which component version to use
apiVersion: delivery.ocm.software/v1alpha1
kind: Component
metadata:
  name: component-name
spec:
  # Reference to the component version used above
  component: ocm.software/ocm-k8s-toolkit/helm-bootstrap
  repositoryRef:
    name: repository-name
  semver: 1.0.0
  interval: 10m
---
# ResourceGraphDefinition to orchestrate the deployment
apiVersion: delivery.ocm.software/v1alpha1
kind: Resource
metadata:
  name: resource-rgd-name
spec:
  componentRef:
    name: component-name
  resource:
    byReference:
      resource:
        # Reference to the resource name in the component version
        name: kro-rgd
  interval: 10m
---
# Operator to deploy the ResourceGraphDefinition
apiVersion: delivery.ocm.software/v1alpha1
kind: OCMDeployer
metadata:
  name: ocmdeployer-name
spec:
  resourceRef:
    name: resource-rgd-name
  interval: 10m
```

`instance.yaml`

```yaml
apiVersion: kro.run/v1alpha1
kind: Bootstrap
metadata:
  name: helm-bootstrap
spec:
  releaseName: "bootstrap-instance"
  message: "Hello from the instance!"
```

#### Pros

* Kro and FluxCD provide the same functionality as the configuration and localization controller. But instead of
  localizing and configuring the resource and then storing it somewhere to be processed, the resource is configured and
  localized within the `ResourceGraphDefinition` and FluxCDs `HelmRelease.spec.values` or `Kustomization.spec.path`.
  Accordingly, our current controllers for localization and configuration could be omitted.
  * As a result, the internal storage can be omitted as well as we do not need to download the resources to
    configure or localize them and make them available again.
    * By omitting the internal storage,
      * we can omit the storage implementation.
      * users do not have to maintain an OCI storage in their production cluster.
      * it gets easier to debug.
  * We can omit the CRDs `ResourceConfig`, `ConfiguredResource`, `LocalizationConfig`, and `LocalizedResource`.
  * The codebase would get simpler as the deployment logic is outsourced.
* With Kros `ResourceGraphDefinition` the developer can create the deployment-instructions and pack them into the
  OCM component version.
* The developer can use any kind of deployer (FluxCD, ArgoCD, ...) that fits their needs by specifying the
  `ResourceGraphDefinition` accordingly.
* Kro can be used to orchestrate the deployment of resources that are dependent on each other. The resources are
  then deployed in such an order that the dependencies are satisfied.

#### Cons

* Kro is a new dependency and is not production-ready and [APIs may change][kro-alpha-stage]. We already found some
  issues. However, the project is open for contributions and some are already accepted:
  * [No possibility to refer to properties within a property of type `json`](https://github.com/open-component-model/ocm-project/issues/455): Open
  * [Kro does not reconcile instances on `ResourceGraphDefinition` changes](https://github.com/open-component-model/ocm-project/issues/451): Fixed
  * [Instances of an RGD are not deleted on RGD-deletion](https://kubernetes.slack.com/archives/C081TMY9D6Y/p1744098078849929): Open
  * Deletion handling in general
    * Example 1: I create resource `a` in a cluster. Then, I create a RGD with the same resource `a` and its instance.
      Now, when I delete my instance, resource `a` is also deleted, although, the original manifest of resource `a` that
      I applied previously was not changed.
    * Example 2: I create an RGD with the same resource `a`. Then, I create two instances of that RGD. Now, when I
      delete one instance, resource `a` is deleted, even though the other instance is still present.
    * Check out this [spike](https://github.com/open-component-model/ocm-project/issues/456) for more details.
  * How are updates of resources handled?
  * What about drift detection of instances?
  * What happens if the `ResourceGraphDefinition`-scheme changes (when an instance is already deployed)?
    * Local testing: We created a `ResourceGraphDefinition` with a `schema.spec`-field of type `string` and deployed the
      RGD as well as an instance of that. Afterwards, we changed the field type to `integer` and reapplied the RGD
      manifest.
      The RGD was reconciled and updated the CRD. The instance did not error, but editing the instance by using the old
      datatype was not possible (usual error of a wrong datatype). However, the original value (of type `string`) was
      still present.
    * How is the migration path?
  * It is not possible reference external resources like `configmap[namespace/name].data`.
    * Already a ["Mega Feature" (request)](https://github.com/kro-run/kro/issues/72), but still open.
* In scenarios, in which the developer has to provide deployment instructions with a `ResourceGraphDefinition` within
  the component version, the developer must be familiar with the deployment technology (e.g. FluxCD).
  * To make it clearer: Previously, the configuration and localisation were processed in our controllers and the result
    was stored in an internal storage. Thus, the developer only had to provide the resources, rules, and configurations.
    The developer was not required to know about the deployment technology.

### FluxDeployer

This approach requires the localization and configuration controllers along with internal storage.
However, in the approach using [Kro](#kro), these components can be omitted as the localization and configuration can
be achieved using Kros `ResourceGraphDefinition` and FluxCDs `HelmRelease.spec.values` or `Kustomization.spec.patch`
(ArgoCD offers similar functionalities). Because the internal storage was mainly used for our own localization and
configuration, we can omit it as well.
This is why, we will not elaborate further on this approach.

### Deployer

This approach requires the localization and configuration controllers along with internal storage.
However, in the approach using [Kro](#kro), these components can be omitted as the localization and configuration can
be achieved using Kros `ResourceGraphDefinition` and FluxCDs `HelmRelease.spec.values` or `Kustomization.spec.patch`
(ArgoCD offers similar functionalities). Because the internal storage was mainly used for our own localization and
configuration, we can omit it as well.
This is why, we will not elaborate further on this approach.

# Links

* Epic [#404](https://github.com/open-component-model/ocm-k8s-toolkit/issues/147)
* Issue [#90](https://github.com/open-component-model/ocm-k8s-toolkit/issues/136)

[kro-github]: https://github.com/kro-run/kro

[kro-doc]: https://kro.run/docs/overview

[kro-alpha-stage]: https://github.com/kro-run/kro/blob/965cb76668433742033c0e413e3cc756ef86d89a/website/docs/docs/getting-started/01-Installation.md?plain=1#L21

