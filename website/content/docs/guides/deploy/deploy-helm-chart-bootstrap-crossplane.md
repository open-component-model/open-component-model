---
title: Deploy a Helm Chart with Crossplane and GitOps
description: "Bootstrap a Helm chart deployment with OCM and Crossplane, composing GitOps deployer objects (Flux or Argo CD) directly from an XRD and Composition for repeatable localized installs."
weight: 55
toc: true
aliases:
  - /docs/tutorials/deploy-helm-chart-bootstrap-crossplane/
  - /docs/tutorials/deploy-an-application-from-a-helm-chart-with-ocm-and-crossplane/
seo:
  description: "Bootstrap a Helm chart deployment with OCM and Crossplane: compose GitOps deployer objects from an XRD and Composition for repeatable localized installs."
---

## What You'll Learn

Your application ships as a Helm chart, and you want its delivery to be repeatable: one versioned OCM component that
carries the chart, its container image, and the instructions to deploy it, with image references that localize
themselves as the component moves between registries. This tutorial builds that deployment on **Crossplane**, using
a `CompositeResourceDefinition` (XRD) plus a `Composition` to reconcile the deployment graph. The XRD registers the
`Bootstrap` schema, and the `Composition` creates the OCM and GitOps deployer objects (Flux or Argo CD) directly. We
will use [Podinfo](https://github.com/stefanprodan/podinfo) as the example application.

{{< callout context="note" title="Coming from the kro tutorial?" icon="outline/info-circle" >}}
This tutorial reaches the same end state as [Deploy an Application from a Helm Chart with OCM and
kro]({{< relref "/docs/guides/deploy/deploy-helm-chart-with-kro-and-gitops.md" >}}), swapping kro's `ResourceGraphDefinition` for a Crossplane
XRD + Composition. Reading the kro tutorial first is **not** required.
{{< /callout >}}

By the end, you'll have:

- An OCM component containing a Helm chart, an image reference, and a Crossplane XRD + Composition
- A running Podinfo application deployed by applying the `Bootstrap` composite resource
- Understanding of how localization keeps image references in sync after transfers

## Prerequisites

{{< callout context="note" title="Set up your environment" icon="outline/settings-check" >}}
Before starting, make sure you have set up your environment as described in the [setup guide]({{< relref "/docs/getting-started/setup-controller-environment.md" >}}).
{{< /callout >}}

- [Controller environment]({{< relref "/docs/getting-started/setup-controller-environment.md" >}}) with OCM Controllers installed
- **Crossplane** installed as the orchestrator, following [Set up Controller Environments > Install an
  Orchestrator]({{< relref "/docs/getting-started/setup-controller-environment.md#install-an-orchestrator" >}}) (the Crossplane tab)
- A **GitOps deployer** installed, either **Flux** or **Argo CD**, following [Set up Controller Environments > Install a
  Deployer]({{< relref "/docs/getting-started/setup-controller-environment.md#install-a-deployer" >}}). Crossplane composes that deployer's
  objects directly: the Flux `OCIRepository` and `HelmRelease`, or the Argo CD `Application`. The matching controllers
  must be running (Flux's `source-controller` and `helm-controller`, or Argo CD's `application-controller`). For Argo
  CD, apply the `Bootstrap` in the `argocd` namespace (via `BOOTSTRAP_NAMESPACE`, see Environment Setup), so no extra
  Argo CD configuration is needed: the composed `Application` lands where stock Argo CD already watches.
- [Custom RBAC]({{< relref "/docs/guides/deploy/configure-custom-rbac.md" >}}) with **both** Crossplane grants, which live in two separate places in
  that guide:
  - The **Crossplane** tab under [Create a ClusterRole and ClusterRoleBinding]({{< relref "/docs/guides/deploy/configure-custom-rbac.md" >}}#tabs-ocm-controller-rbac-1)
    gives the OCM controller read access (`get`, `list`, `watch`) on `compositions` and `compositeresourcedefinitions`
    (`apiextensions.crossplane.io`) so it can check the deployed objects for drift. The `bootstrap-deployer` service
    account gets its own write access from the `ClusterRole` in `bootstrap.yaml` later in this tutorial.
  - The [Crossplane subsection under RBAC for the orchestrator's own service account]({{< relref "/docs/guides/deploy/configure-custom-rbac.md#crossplane" >}})
    lets Crossplane's own service account compose the OCM `Resource` and the deployer objects (Flux or Argo CD).
- The [Patch and Transform function](https://github.com/crossplane-contrib/function-patch-and-transform)
  (`function-patch-and-transform`), which the `Composition` runs as its pipeline step. Install it via the Crossplane
  tab of [Set up Controller Environments > Install an Orchestrator]({{< relref "/docs/getting-started/setup-controller-environment.md#install-an-orchestrator" >}}).
- [OCM CLI]({{< relref "/docs/getting-started/ocm-cli-installation.md" >}}) installed
- Access to an OCI registry (e.g., [ghcr.io](https://docs.github.com/en/packages/learn-github-packages/introduction-to-github-packages))
- `envsubst` installed (pre-installed on most Linux/macOS systems as part of `gettext`)

{{< callout context="note" title="Private registries" icon="outline/lock" >}}
If using a private registry, you'll need to configure credentials for both the OCM CLI and the controller resources. See [Configure Credentials for Controllers]({{< relref "/docs/guides/deploy/configure-controller-credentials.md" >}}) for details.
{{< /callout >}}

## Environment Setup

Set environment variables for your GitHub username, OCM repository, and the namespace the composite runs in. Use a
lowercase GitHub username, OCI registries such as `ghcr.io` reject uppercase letters in the repository path:

```bash
export GITHUB_USERNAME=<your-github-username>
export OCM_REPO=ghcr.io/$GITHUB_USERNAME/ocm-tutorial
# Namespace the Bootstrap composite and its composed objects run in.
# Flux: default. Argo CD: argocd, so stock Argo CD watches the composed Application.
export BOOTSTRAP_NAMESPACE=default   # set to argocd for the Argo CD deployer
```

## Concepts

### The Bootstrap Pattern

Instead of applying the deployment graph (the XRD + Composition) to the cluster by hand, the **bootstrap pattern**
packages that graph as a resource *inside* the OCM component. The OCM Deployer controller then extracts the blob from
the component and applies it for you. The deployment instructions travel with the component and stay versioned
alongside the chart and image, so every environment installs the same graph. In this tutorial the packaged blob is a
Crossplane XRD + Composition.

### How the Pieces Fit Together

The **graph engine** is what reconciles the deployment graph and creates the deployer objects. This tutorial ships a
Crossplane `CompositeResourceDefinition` (XRD) plus a `Composition`. The XRD registers a `Bootstrap` composite
kind. When you apply a `Bootstrap` resource, Crossplane resolves the chart and image through OCM `Resource` objects,
then composes the deployer objects directly: for Flux an `OCIRepository` and a `HelmRelease`, for Argo CD a single
`Application`. The deployer then renders the chart into the running workload.

The OCM `Resource` and the deployer objects are composed directly by the Composition, with no intermediate wrapper.

If you already know the kro variant, the building blocks map like this (otherwise skip the table):

| kro                                        | Crossplane                                                           |
| ------------------------------------------ | -------------------------------------------------------------------- |
| `ResourceGraphDefinition`                  | `CompositeResourceDefinition` (XRD) + `Composition`                  |
| Schema `kind: Bootstrap` registered by kro | Composite `kind: Bootstrap` registered by the XRD                    |
| Instance of the `Bootstrap` CRD            | A `Bootstrap` composite resource                                     |
| kro reconciles the graph                   | Crossplane reconciles the composite, creating the composed resources |

The `Composition` runs in **pipeline mode** (`mode: Pipeline`). A single pipeline step invokes the `function-patch-and-transform` function, whose
input holds the patch-and-transform resource list. The resolved chart and image OCI coordinates travel from the OCM
`Resource` objects to the deployer objects through the composite's `status.atProvider` fields, since patch-and-transform
wires values only between a composed resource and the composite, never directly between two composed resources.

### Localization

**Localization** keeps image references in sync when a component moves between registries. Transferring the component
rewrites the image references in its descriptor to point at the target registry. At deploy time the **Composition**
reads the resolved OCM `Resource` status and injects the current image into the Helm values, so the workload always
runs the image from the registry it was transferred to, not the original upstream location.

## Create and Publish a Component Version

First, create an OCM component version containing three resources:

- **helm-resource**: The Podinfo Helm chart
- **image-resource**: The Podinfo container image (for localization)
- **composition**: Deployment instructions (the Crossplane XRD + Composition)

{{< steps >}}
{{< step >}}

### Create a working directory

```shell
mkdir /tmp/bootstrap-deploy-crossplane && cd /tmp/bootstrap-deploy-crossplane
```

{{< /step >}}

{{< step >}}

### Define the component

Create a `component-constructor.yaml` file:

```shell
cat > component-constructor.yaml << 'EOF'
components:
  - name: ocm.software/ocm-k8s-toolkit/bootstrap-crossplane
    version: "1.0.0"
    provider:
      name: ocm.software
    resources:
      - name: helm-resource
        type: helmChart
        version: "1.0.0"
        access:
          type: OCIImage/v1
          imageReference: "ghcr.io/stefanprodan/charts/podinfo:6.11.1@sha256:a9b2804ec61795a7457b2303bf9efbc5fba51f856c3945f3bb0af68bf3b35afd"
      - name: image-resource
        type: ociArtifact
        version: "1.0.0"
        access:
          type: OCIImage/v1
          imageReference: "ghcr.io/stefanprodan/podinfo:6.11.1@sha256:8fa56908408de98f24aed2a162b1bb42c0b98df7abfcc5a76a14a8be510457c5"
      - name: composition
        type: blob
        version: "1.0.0"
        input:
          type: File/v1
          path: ./composition.yaml
EOF
```

The resource `composition` is of type `blob` and points to a file `composition.yaml`. For
this variant that file holds a Crossplane XRD + Composition. The XRD and the two OCM `Resource` objects are the same
for both deployers. The Composition's last composed resources differ: Flux uses an `OCIRepository` plus a
`HelmRelease`, Argo CD uses a single `Application`. Pick the deployer you installed and create `composition.yaml`
before building the component version:

{{< tabs "composition-deployer" >}}
{{< tab "Flux" >}}

{{< details "XRD + Composition (composition.yaml)" >}}
The two OCM `Resource` objects (`ocm-resource-chart` and `ocm-resource-image`) resolve the chart and image through
the OCM controllers, using `additionalStatusFields`/`toOCI()` to expose the registry, repository, and digest under
`status.additional`. See [Concept: OCM Controllers]({{< relref "docs/concepts/ocm-controllers.md#additional-status-fields" >}})
for how that mechanism works. `ToCompositeFieldPath` patches copy those resolved coordinates onto the composite
status, where the composed Flux `OCIRepository` and `HelmRelease` read them.

```shell
cat > composition.yaml << 'EOF'
apiVersion: apiextensions.crossplane.io/v2
kind: CompositeResourceDefinition
metadata:
  name: bootstraps.ocm.software
spec:
  group: ocm.software
  names:
    kind: Bootstrap
    plural: bootstraps
  # Namespaced is the recommended scope for app delivery. It keeps the composite
  # and its composed resources in one namespace, BOOTSTRAP_NAMESPACE (default for
  # Flux). Flux watches all namespaces, so the composed HelmRelease reconciles there.
  scope: Namespaced
  versions:
    - name: v1alpha1
      served: true
      referenceable: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                # Name of the OCM Component CR created by the bootstrap resources below.
                ocmComponent:
                  type: string
                  default: bootstrap-component
            status:
              type: object
              properties:
                atProvider:
                  type: object
                  properties:
                    # Resolved chart OCI coordinates, written back by the chart Resource patches.
                    chartRegistry:
                      type: string
                    chartRepository:
                      type: string
                    chartDigest:
                      type: string
                    # Resolved image OCI coordinates, used for localization.
                    imageRegistry:
                      type: string
                    imageRepository:
                      type: string
                    imageDigest:
                      type: string
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: bootstrap
  labels:
    crossplane.io/xrd: bootstraps.ocm.software
spec:
  compositeTypeRef:
    apiVersion: ocm.software/v1alpha1
    kind: Bootstrap
  mode: Pipeline
  pipeline:
    - step: patch-and-transform
      functionRef:
        name: function-patch-and-transform
      input:
        apiVersion: pt.fn.crossplane.io/v1beta1
        kind: Resources
        resources:
          # 1. OCM Resource (chart): resolve the Helm chart OCI reference. The
          #    composed OCM Resource inherits the composite's namespace.
          #    additionalStatusFields/toOCI() exposes registry, repository, and
          #    digest under status.additional.
          - name: ocm-resource-chart
            base:
              apiVersion: delivery.ocm.software/v1alpha1
              kind: Resource
              spec:
                resource:
                  byReference:
                    resource:
                      name: helm-resource
                additionalStatusFields:
                  oci: resource.access.toOCI()
            patches:
              - type: FromCompositeFieldPath
                fromFieldPath: spec.ocmComponent
                toFieldPath: spec.componentRef.name
              # Derive a composite-specific Resource name so parallel composites
              # never share one default/<name> OCM Resource.
              - type: FromCompositeFieldPath
                fromFieldPath: metadata.name
                toFieldPath: metadata.name
                transforms:
                  - type: string
                    string:
                      type: Format
                      fmt: "%s-chart"
              # Publish resolved chart coordinates onto composite status so the
              # OCIRepository below can consume them. patch-and-transform wires
              # values only between a composed resource and the composite, never
              # directly between two composed resources, so this round-trip stays.
              - type: ToCompositeFieldPath
                fromFieldPath: status.additional.oci.registry
                policy:
                  fromFieldPath: Optional
                toFieldPath: status.atProvider.chartRegistry
              - type: ToCompositeFieldPath
                fromFieldPath: status.additional.oci.repository
                policy:
                  fromFieldPath: Optional
                toFieldPath: status.atProvider.chartRepository
              - type: ToCompositeFieldPath
                fromFieldPath: status.additional.oci.digest
                policy:
                  fromFieldPath: Optional
                toFieldPath: status.atProvider.chartDigest
          # 2. OCM Resource (image): resolve the container image OCI reference for
          #    localization. Composed directly, same as the chart resource above.
          - name: ocm-resource-image
            base:
              apiVersion: delivery.ocm.software/v1alpha1
              kind: Resource
              spec:
                resource:
                  byReference:
                    resource:
                      name: image-resource
                additionalStatusFields:
                  oci: resource.access.toOCI()
            patches:
              - type: FromCompositeFieldPath
                fromFieldPath: spec.ocmComponent
                toFieldPath: spec.componentRef.name
              # Derive a composite-specific Resource name so parallel composites
              # never share one default/<name> OCM Resource.
              - type: FromCompositeFieldPath
                fromFieldPath: metadata.name
                toFieldPath: metadata.name
                transforms:
                  - type: string
                    string:
                      type: Format
                      fmt: "%s-image"
              - type: ToCompositeFieldPath
                fromFieldPath: status.additional.oci.registry
                policy:
                  fromFieldPath: Optional
                toFieldPath: status.atProvider.imageRegistry
              - type: ToCompositeFieldPath
                fromFieldPath: status.additional.oci.repository
                policy:
                  fromFieldPath: Optional
                toFieldPath: status.atProvider.imageRepository
              - type: ToCompositeFieldPath
                fromFieldPath: status.additional.oci.digest
                policy:
                  fromFieldPath: Optional
                toFieldPath: status.atProvider.imageDigest
          # 3. Flux OCIRepository, composed directly by Crossplane. The chart URL
          #    and digest come from the composite status populated in step 1. Those
          #    patches use policy.fromFieldPath: Required so Crossplane withholds this
          #    resource until the OCM Resource has resolved. Without it the OCIRepository
          #    would be created with the placeholder url and Flux would reject it.
          - name: ocirepository
            base:
              apiVersion: source.toolkit.fluxcd.io/v1
              kind: OCIRepository
              spec:
                interval: 1m0s
                layerSelector:
                  mediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip"
                  operation: copy
                url: placeholder  # overwritten by patch below
                ref:
                  digest: placeholder  # overwritten by patch below
            patches:
              - type: FromCompositeFieldPath
                fromFieldPath: metadata.name
                toFieldPath: metadata.name
                transforms:
                  - type: string
                    string:
                      type: Format
                      fmt: "%s-ocirepository"
              - type: CombineFromComposite
                combine:
                  variables:
                    - fromFieldPath: status.atProvider.chartRegistry
                    - fromFieldPath: status.atProvider.chartRepository
                  strategy: string
                  string:
                    fmt: "oci://%s/%s"
                policy:
                  fromFieldPath: Required
                toFieldPath: spec.url
              - type: FromCompositeFieldPath
                fromFieldPath: status.atProvider.chartDigest
                policy:
                  fromFieldPath: Required
                toFieldPath: spec.ref.digest
          # 4. Flux HelmRelease, composed directly by Crossplane. Localization
          #    happens here: the image repository and a pseudo-tag+digest are
          #    injected into the chart values from composite status, exactly as the
          #    kro RGD's helmrelease resource does.
          - name: helmrelease
            base:
              apiVersion: helm.toolkit.fluxcd.io/v2
              kind: HelmRelease
              spec:
                releaseName: placeholder  # overwritten by patch below
                interval: 1m
                timeout: 5m
                # Deploy the workload into its own namespace, created by Flux.
                targetNamespace: podinfo
                install:
                  createNamespace: true
                chartRef:
                  kind: OCIRepository
                  name: placeholder  # overwritten by patch below
            patches:
              - type: FromCompositeFieldPath
                fromFieldPath: metadata.name
                toFieldPath: metadata.name
                transforms:
                  - type: string
                    string:
                      type: Format
                      fmt: "%s-helmrelease"
              # Derive a composite-specific, reader-deterministic Helm release name
              # from the composite name so parallel composites never share a release.
              - type: FromCompositeFieldPath
                fromFieldPath: metadata.name
                toFieldPath: spec.releaseName
                transforms:
                  - type: string
                    string:
                      type: Format
                      fmt: "%s-release"
              # Point the HelmRelease at the OCIRepository created in step 3.
              - type: FromCompositeFieldPath
                fromFieldPath: metadata.name
                toFieldPath: spec.chartRef.name
                transforms:
                  - type: string
                    string:
                      type: Format
                      fmt: "%s-ocirepository"
              # Localized image: registry/repository from the resolved image resource.
              - type: CombineFromComposite
                combine:
                  variables:
                    - fromFieldPath: status.atProvider.imageRegistry
                    - fromFieldPath: status.atProvider.imageRepository
                  strategy: string
                  string:
                    fmt: "%s/%s"
                policy:
                  fromFieldPath: Required
                toFieldPath: spec.values.image.repository
              # Pseudo-tag with @digest. Podinfo's chart builds repository:tag, and OCI
              # runtimes ignore the tag when a digest is present, so this pins by digest.
              - type: FromCompositeFieldPath
                fromFieldPath: status.atProvider.imageDigest
                policy:
                  fromFieldPath: Required
                toFieldPath: spec.values.image.tag
                transforms:
                  - type: string
                    string:
                      type: Format
                      fmt: "latest@%s"
EOF
```

{{< /details >}}
{{< callout context="note" title="Private registries" icon="outline/lock" >}}
If you want to keep the component private instead of making it public after transfer, configure credentials for the
OCM Controllers and Flux before running `ocm add cv`. See
[Credentials for OCM Controllers]({{< relref "/docs/guides/deploy/configure-controller-credentials.md" >}}), and add a
`secretRef` to the `OCIRepository` manifest in the Composition's `ocirepository` resource.
{{< /callout >}}

{{< /tab >}}
{{< tab "Argo CD" >}}

{{< details "XRD + Composition (composition.yaml)" >}}
The XRD and the two OCM `Resource` objects (`ocm-resource-chart` and `ocm-resource-image`) are identical to the Flux
variant. They resolve the chart and image through the OCM controllers and publish the registry, repository, and
digest onto the composite status. The final composed resource is an Argo CD `Application` instead of Flux objects.
Localization happens in `spec.source.helm.valuesObject`, where the resolved image coordinates are injected from
composite status.

```shell
cat > composition.yaml << 'EOF'
apiVersion: apiextensions.crossplane.io/v2
kind: CompositeResourceDefinition
metadata:
  name: bootstraps.ocm.software
spec:
  group: ocm.software
  names:
    kind: Bootstrap
    plural: bootstraps
  # Namespaced is the recommended scope for app delivery. It keeps the composite
  # and its composed resources in one namespace, BOOTSTRAP_NAMESPACE (argocd for
  # Argo CD) so the composed Application lands where stock Argo CD watches.
  scope: Namespaced
  versions:
    - name: v1alpha1
      served: true
      referenceable: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                # Name of the OCM Component CR created by the bootstrap resources below.
                ocmComponent:
                  type: string
                  default: bootstrap-component
            status:
              type: object
              properties:
                atProvider:
                  type: object
                  properties:
                    # Resolved chart OCI coordinates, written back by the chart Resource patches.
                    chartRegistry:
                      type: string
                    chartRepository:
                      type: string
                    chartTag:
                      type: string
                    # Resolved image OCI coordinates, used for localization.
                    imageRegistry:
                      type: string
                    imageRepository:
                      type: string
                    imageDigest:
                      type: string
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: bootstrap
  labels:
    crossplane.io/xrd: bootstraps.ocm.software
spec:
  compositeTypeRef:
    apiVersion: ocm.software/v1alpha1
    kind: Bootstrap
  mode: Pipeline
  pipeline:
    - step: patch-and-transform
      functionRef:
        name: function-patch-and-transform
      input:
        apiVersion: pt.fn.crossplane.io/v1beta1
        kind: Resources
        resources:
          # 1. OCM Resource (chart): resolve the Helm chart OCI reference. The
          #    composed OCM Resource inherits the composite's namespace.
          - name: ocm-resource-chart
            base:
              apiVersion: delivery.ocm.software/v1alpha1
              kind: Resource
              spec:
                resource:
                  byReference:
                    resource:
                      name: helm-resource
                additionalStatusFields:
                  oci: resource.access.toOCI()
            patches:
              - type: FromCompositeFieldPath
                fromFieldPath: spec.ocmComponent
                toFieldPath: spec.componentRef.name
              # Derive a composite-specific Resource name so parallel composites
              # never share one default/<name> OCM Resource.
              - type: FromCompositeFieldPath
                fromFieldPath: metadata.name
                toFieldPath: metadata.name
                transforms:
                  - type: string
                    string:
                      type: Format
                      fmt: "%s-chart"
              # Publish resolved chart coordinates onto composite status so the
              # Application below can consume them.
              - type: ToCompositeFieldPath
                fromFieldPath: status.additional.oci.registry
                policy:
                  fromFieldPath: Optional
                toFieldPath: status.atProvider.chartRegistry
              - type: ToCompositeFieldPath
                fromFieldPath: status.additional.oci.repository
                policy:
                  fromFieldPath: Optional
                toFieldPath: status.atProvider.chartRepository
              # Argo CD resolves the chart by tag, not digest, so publish the tag.
              - type: ToCompositeFieldPath
                fromFieldPath: status.additional.oci.tag
                policy:
                  fromFieldPath: Optional
                toFieldPath: status.atProvider.chartTag
          # 2. OCM Resource (image): resolve the container image OCI reference for
          #    localization. Composed directly, same as the chart resource above.
          - name: ocm-resource-image
            base:
              apiVersion: delivery.ocm.software/v1alpha1
              kind: Resource
              spec:
                resource:
                  byReference:
                    resource:
                      name: image-resource
                additionalStatusFields:
                  oci: resource.access.toOCI()
            patches:
              - type: FromCompositeFieldPath
                fromFieldPath: spec.ocmComponent
                toFieldPath: spec.componentRef.name
              - type: FromCompositeFieldPath
                fromFieldPath: metadata.name
                toFieldPath: metadata.name
                transforms:
                  - type: string
                    string:
                      type: Format
                      fmt: "%s-image"
              - type: ToCompositeFieldPath
                fromFieldPath: status.additional.oci.registry
                policy:
                  fromFieldPath: Optional
                toFieldPath: status.atProvider.imageRegistry
              - type: ToCompositeFieldPath
                fromFieldPath: status.additional.oci.repository
                policy:
                  fromFieldPath: Optional
                toFieldPath: status.atProvider.imageRepository
              - type: ToCompositeFieldPath
                fromFieldPath: status.additional.oci.digest
                policy:
                  fromFieldPath: Optional
                toFieldPath: status.atProvider.imageDigest
          # 3. Argo CD Application, composed directly by Crossplane. The chart
          #    repoURL/targetRevision and the localized image values are patched
          #    from the composite status populated above.
          - name: application
            base:
              apiVersion: argoproj.io/v1alpha1
              kind: Application
              metadata:
                # No namespace here: Crossplane composes the Application into the
                # composite's namespace. Apply the Bootstrap in argocd so stock
                # Argo CD watches it (see BOOTSTRAP_NAMESPACE in Environment Setup).
                finalizers:
                  - resources-finalizer.argocd.argoproj.io
              spec:
                project: default
                source:
                  chart: podinfo
                  repoURL: placeholder  # overwritten by patch below
                  targetRevision: placeholder  # overwritten by patch below
                  helm:
                    releaseName: placeholder  # overwritten by patch below
                destination:
                  server: https://kubernetes.default.svc
                  # Deploy the workload into its own namespace, created on sync
                  # by the CreateNamespace=true sync option below.
                  namespace: podinfo
                syncPolicy:
                  automated:
                    prune: true
                    selfHeal: true
                  syncOptions:
                    - CreateNamespace=true
            patches:
              - type: FromCompositeFieldPath
                fromFieldPath: metadata.name
                toFieldPath: metadata.name
                transforms:
                  - type: string
                    string:
                      type: Format
                      fmt: "%s-application"
              # Derive a composite-specific Helm release name from the composite
              # name so parallel composites never share a release.
              - type: FromCompositeFieldPath
                fromFieldPath: metadata.name
                toFieldPath: spec.source.helm.releaseName
                transforms:
                  - type: string
                    string:
                      type: Format
                      fmt: "%s-release"
              # Chart repoURL from the resolved chart coordinates.
              - type: CombineFromComposite
                combine:
                  variables:
                    - fromFieldPath: status.atProvider.chartRegistry
                    - fromFieldPath: status.atProvider.chartRepository
                  strategy: string
                  string:
                    fmt: "oci://%s/%s"
                policy:
                  fromFieldPath: Required
                toFieldPath: spec.source.repoURL
              - type: FromCompositeFieldPath
                fromFieldPath: status.atProvider.chartTag
                policy:
                  fromFieldPath: Required
                toFieldPath: spec.source.targetRevision
              # Localized image: registry/repository from the resolved image resource.
              - type: CombineFromComposite
                combine:
                  variables:
                    - fromFieldPath: status.atProvider.imageRegistry
                    - fromFieldPath: status.atProvider.imageRepository
                  strategy: string
                  string:
                    fmt: "%s/%s"
                policy:
                  fromFieldPath: Required
                toFieldPath: spec.source.helm.valuesObject.image.repository
              # Pseudo-tag with @digest. Podinfo's chart builds repository:tag, and OCI
              # runtimes ignore the tag when a digest is present, so this pins by digest.
              - type: FromCompositeFieldPath
                fromFieldPath: status.atProvider.imageDigest
                policy:
                  fromFieldPath: Required
                toFieldPath: spec.source.helm.valuesObject.image.tag
                transforms:
                  - type: string
                    string:
                      type: Format
                      fmt: "latest@%s"
            # Argo CD Application objects never set a standard Ready condition.
            # Without these checks Crossplane waits for status.conditions[Ready]
            # forever and the Bootstrap stays READY=False. Treat the Application
            # as ready only when Argo CD reports it Healthy and Synced.
            readinessChecks:
              - type: MatchString
                fieldPath: status.health.status
                matchString: Healthy
              - type: MatchString
                fieldPath: status.sync.status
                matchString: Synced
EOF
```

{{< /details >}}
{{< callout context="note" title="Private registries" icon="outline/lock" >}}
If you want to keep the component private instead of making it public after transfer, configure credentials for the
OCM Controllers before running `ocm add cv`. See
[Credentials for OCM Controllers]({{< relref "/docs/guides/deploy/configure-controller-credentials.md" >}}). For Argo CD,
pull credentials are configured on the Argo CD repository, not in the Composition. See the
[Argo CD private registry docs](https://argo-cd.readthedocs.io/en/stable/operator-manual/declarative-setup/#repositories).
{{< /callout >}}

{{< /tab >}}
{{< /tabs >}}
{{< /step >}}

{{< step >}}

### Build and transfer the component

Build the component version locally:

```bash
ocm add cv
```

Transfer to your registry with the OCI uploader to enable localization. An OCI uploader entry makes the Helm chart and image land as OCI artifacts in the target registry, keeping image references the Composition can rewrite. A local blob catch-all copies every other resource:

```yaml
cat > .ocmconfig << 'EOF'
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
EOF
```

> **Note:** The CLI merges `.ocmconfig` from the current directory with your other OCM configuration (such as `$HOME/.ocmconfig`), so credentials and resolvers stay in effect.

```bash
ocm transfer cv transport-archive//ocm.software/ocm-k8s-toolkit/bootstrap-crossplane:1.0.0 $OCM_REPO
```

Now that the component is in your registry, make it public so the OCM Controllers and Flux can pull it without
credentials: open the `component-descriptors/ocm.software/ocm-k8s-toolkit/bootstrap-crossplane` package in the GitHub
`packages` tab and set its visibility to `public`. To keep it private instead, follow the private-registry note above.

{{< /step >}}

{{< step >}}

### Verify the transfer

Check that the component was transferred and resources were localized:

```bash
ocm get cv $OCM_REPO//ocm.software/ocm-k8s-toolkit/bootstrap-crossplane:1.0.0 -o yaml | grep imageReference
```

You should see image references pointing to `$OCM_REPO/...` instead of the original locations. This confirms localization worked.
{{< /step >}}
{{< /steps >}}

## Deploy the Helm Chart

Now create the bootstrap resources that fetch and apply the XRD + Composition from the component. This chain is
identical to the kro variant. The `NamespacedDeployer` applies whatever blob the component carries with its own
service account, so nothing here is Crossplane-specific.

{{< steps >}}
{{< step >}}

### Create bootstrap resources

Create `bootstrap.yaml`:

{{< callout context="note" title="Private registries" icon="outline/lock" >}}
If you chose to keep your package private, do not forget to add the secret to the repository's `ocmConfig`, as described in
[Credentials for OCM Controllers]({{< relref "/docs/guides/deploy/configure-controller-credentials.md" >}}).
{{< /callout >}}

{{< details "Bootstrap Resources (bootstrap.yaml)" >}}

```shell
cat > bootstrap.yaml << 'EOF'
apiVersion: delivery.ocm.software/v1alpha1
kind: Repository
metadata:
  name: bootstrap-repository
  # Resource.componentRef and Component.repositoryRef are same-namespace
  # references, so the whole OCM chain must share the composite's namespace.
  namespace: ${BOOTSTRAP_NAMESPACE}
spec:
  repositorySpec:
    baseUrl: $OCM_REPO
    type: OCIRegistry
  interval: 1m
  # ocmConfig is required, if the OCM repository requires credentials to access it.
  # ocmConfig:
---
apiVersion: delivery.ocm.software/v1alpha1
kind: Component
metadata:
  name: bootstrap-component
  namespace: ${BOOTSTRAP_NAMESPACE}
spec:
  component: ocm.software/ocm-k8s-toolkit/bootstrap-crossplane
  repositoryRef:
    name: bootstrap-repository
  semver: 1.0.0
  interval: 1m
  # ocmConfig is required, if the OCM repository requires credentials to access it.
  # ocmConfig:
---
apiVersion: delivery.ocm.software/v1alpha1
kind: Resource
metadata:
  name: bootstrap-composition
  namespace: ${BOOTSTRAP_NAMESPACE}
spec:
  componentRef:
    name: bootstrap-component
  resource:
    byReference:
      resource:
        name: composition
  # ocmConfig is required, if the OCM repository requires credentials to access it.
  # ocmConfig:
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: bootstrap-deployer
  namespace: ${BOOTSTRAP_NAMESPACE}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: bootstrap-deployer
rules:
  # The XRD and Composition are cluster-scoped, so the service account needs a
  # ClusterRole and ClusterRoleBinding rather than a namespaced Role.
  - apiGroups: ["apiextensions.crossplane.io"]
    resources: ["compositeresourcedefinitions", "compositions"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: bootstrap-deployer
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: bootstrap-deployer
subjects:
  - kind: ServiceAccount
    name: bootstrap-deployer
    namespace: ${BOOTSTRAP_NAMESPACE}
---
apiVersion: delivery.ocm.software/v1alpha1
kind: NamespacedDeployer
metadata:
  name: bootstrap-deployer
  namespace: ${BOOTSTRAP_NAMESPACE}
spec:
  resourceRef:
    # Reference to the OCM resource that contains the XRD + Composition blob.
    # NamespacedDeployer is namespace-scoped, so this is a same-namespace reference.
    name: bootstrap-composition
  # The deployer applies the XRD + Composition with the permissions of this service account.
  serviceAccountName: bootstrap-deployer
  # ocmConfig is required, if the OCM repository requires credentials to access it.
  # ocmConfig:
EOF
```

{{< /details >}}
{{< /step >}}

{{< step >}}

### Apply the bootstrap resources

{{< callout context="caution" title="RBAC required before you apply" icon="outline/alert-triangle" >}}
The `bootstrap-deployer` service account above gets write access from the `ClusterRole` in `bootstrap.yaml`. The controller also watches the deployed objects for drift with its own identity, so it still needs `get`, `list` and `watch` on the XRD and Composition. Follow [Configure Custom RBAC for Deployers]({{< relref "/docs/guides/deploy/configure-custom-rbac.md#grant-the-controller-read-access" >}}) to grant them before you apply.
{{< /callout >}}

```bash
envsubst < bootstrap.yaml | kubectl apply -f -
```

Wait for the XRD to be installed (this may take 30-60 seconds). `kubectl` rejects `-w` with more than one resource
type, so watch the XRD on its own:

```bash
kubectl get xrd -w
```

Once the XRD is `ESTABLISHED`, check the Composition:

```bash
kubectl get composition
```

<details>
<summary>You should see this output</summary>

```console
NAME                                                     ESTABLISHED   AGE
compositeresourcedefinition.../bootstraps.ocm.software   True          2m56s

NAME                                    XR-KIND     XR-APIVERSION           AGE
composition.apiextensions.../bootstrap  Bootstrap   ocm.software/v1alpha1   2m56s
```

</details>

When the XRD is `ESTABLISHED`, the Deployer has applied the blob and Crossplane has registered the
`Bootstrap` composite kind.
{{< /step >}}

{{< step >}}

### Create the Bootstrap resource

Now create a `Bootstrap` composite resource to trigger the actual deployment. You apply the composite kind directly.
The composite and its composed resources live in `$BOOTSTRAP_NAMESPACE` (`default` for Flux, `argocd` for Argo CD).
`ocmComponent` selects the OCM `Component` CR created by the bootstrap resources
(`bootstrap-component`), matching the XRD's default. Create `bootstrap-xr.yaml`:

```shell
cat > bootstrap-xr.yaml << 'EOF'
apiVersion: ocm.software/v1alpha1
kind: Bootstrap
metadata:
  name: bootstrap
  namespace: ${BOOTSTRAP_NAMESPACE}
spec:
  ocmComponent: bootstrap-component
EOF
```

{{< /step >}}

{{< step >}}

### Deploy the application

Apply the composite to the cluster:

```bash
envsubst < bootstrap-xr.yaml | kubectl apply -f -
```

<details>
<summary>You should see this output</summary>

```console
bootstrap.ocm.software/bootstrap created
```

</details>

Wait for the composite and its composed resources to become ready:

```bash
kubectl get bootstrap.ocm.software -n "$BOOTSTRAP_NAMESPACE" -w
```

<details>
<summary>You should see this output</summary>

```console
NAME        SYNCED   READY   COMPOSITION   AGE
bootstrap   True     True    bootstrap     3m23s
```

</details>

When `SYNCED` and `READY` are both `True`, Crossplane has resolved the OCM resources and composed the deployer objects
directly (the Flux `OCIRepository` and `HelmRelease`, or the Argo CD `Application`), which the deployer then reconciles into the running workload.
{{< /step >}}

{{< step >}}

### Verify localization

Check that the deployed pod uses the localized image from your registry (not the original `ghcr.io/stefanprodan/...`).
The composite can report `READY` before the deployer has reconciled the chart into a running pod, so wait for the pod to
appear first:

```bash
kubectl wait -n podinfo --for=condition=Ready pod -l app.kubernetes.io/name=bootstrap-release-podinfo --timeout=300s
kubectl get pods -n podinfo -l app.kubernetes.io/name=bootstrap-release-podinfo -o jsonpath='{.items[0].spec.containers[0].image}'
```

<details>
<summary>You should see this output</summary>

```console
ghcr.io/$GITHUB_USERNAME/ocm-tutorial/stefanprodan/podinfo:latest@sha256:8fa56908408de98f24aed2a162b1bb42c0b98df7abfcc5a76a14a8be510457c5
```

</details>

The image reference points to your registry with a digest. Localization worked!
{{< /step >}}
{{< /steps >}}

## Troubleshooting

### Authentication Errors (401 Unauthorized)

If you see `401: unauthorized` errors, your GitHub package is private. Either:

- Make the package public in GitHub Package settings
- Configure credentials as described in the "Define the component" step

### XRD or Composition Not Installing

Check controller logs:

```bash
kubectl logs -n ocm-k8s-toolkit-system deployment/ocm-k8s-toolkit-controller-manager
```

Common causes: missing component, wrong repository URL, credential issues, or an RBAC gap. The
`bootstrap-deployer` service account needs write access to apply the XRD and Composition, and the controller needs
read access to watch them for drift. See RBAC Permission Errors below.

### RBAC Permission Errors

The `NamespacedDeployer` applies the XRD and Composition as its `bootstrap-deployer` service account, while the
controller only watches them with its own identity. A `forbidden` error can come from either side:

- If the deployer logs show `cannot create resource`, the `bootstrap-deployer` service account lacks write access.
  It gets that from the `bootstrap-deployer` `ClusterRole` and binding in `bootstrap.yaml`. Confirm the grant with:

  ```bash
  kubectl auth can-i create compositions.apiextensions.crossplane.io \
    --as=system:serviceaccount:${BOOTSTRAP_NAMESPACE}:bootstrap-deployer
  ```

- If the controller logs show it `cannot list` or `cannot watch` the XRD and Composition, the controller lacks read
  access. Apply the `ocm-controller-crossplane` `ClusterRole` and binding from
  [Configure Custom RBAC for Deployers]({{< relref "/docs/guides/deploy/configure-custom-rbac.md#grant-the-controller-read-access" >}})
  (select the **Crossplane** tab), then confirm the grant with:

  ```bash
  kubectl auth can-i watch compositions.apiextensions.crossplane.io \
    --as=system:serviceaccount:ocm-k8s-toolkit-system:ocm-k8s-toolkit-controller-manager
  ```

Separately, if the composite's composed resources stay stuck (see [Composite Not Becoming
Ready](#composite-not-becoming-ready)), the fault is Crossplane's own service account. Crossplane composes the
OCM `Resource` and the deployer objects directly, so Crossplane core must be granted access to those non-Crossplane kinds.
See the next section.

#### Composed resources stay forbidden

Crossplane's service account can create any resource installed by a provider or defined by an XRD, but it must be
granted access to compose any other kind of resource (the OCM `Resource` and the deployer objects here). Without that
grant the composite reports a `forbidden` error, for example

```text
cannot compose resources: ... resources.delivery.ocm.software "<xr>-image" is forbidden: User
"system:serviceaccount:crossplane-system:crossplane" cannot create resource
"resources" in API group "delivery.ocm.software" in the namespace "default"
```

Grant Crossplane's service account access to the composed kinds with an aggregated `ClusterRole`, as described in
[Configure Custom RBAC for Deployers > Crossplane]({{< relref "/docs/guides/deploy/configure-custom-rbac.md#crossplane" >}}).

### Composite Not Becoming Ready

If the `Bootstrap` composite stays `SYNCED=False` or `READY=False`:

```bash
kubectl describe bootstrap.ocm.software bootstrap -n "$BOOTSTRAP_NAMESPACE"
```

Inspect the composed resources directly to see which one is stuck:

```bash
# Flux deployer (composed into $BOOTSTRAP_NAMESPACE, default `default`)
kubectl get resource.delivery.ocm.software,ocirepository,helmrelease -n "$BOOTSTRAP_NAMESPACE"
kubectl describe helmrelease bootstrap-helmrelease -n "$BOOTSTRAP_NAMESPACE"
# Argo CD deployer (composed into $BOOTSTRAP_NAMESPACE, default `argocd`)
kubectl describe application bootstrap-application -n "$BOOTSTRAP_NAMESPACE"
```

Check the Events section and each composed resource's status for error messages. A common cause is Crossplane
lacking RBAC for the composed OCM `Resource` or the deployer CRDs (see [Composed resources stay
forbidden](#composed-resources-stay-forbidden)).

### ImagePullBackOff Errors

If pods show `ImagePullBackOff` or `ErrImagePull` errors, the kubelet cannot pull the localized image from your private registry. Set `image.pullSecrets` (for example `image.pullSecrets: [{name: ghcr-secret}]`) in the chart values of the Composition's deployer resource: the Flux `helmrelease` `spec.values`, or the Argo CD `application` `spec.source.helm.valuesObject`. Podinfo reads pull secrets from `image.pullSecrets`, a top-level `imagePullSecrets` value is ignored.

## What You Learned

You've successfully:

- Created an OCM component with an embedded Crossplane XRD + Composition
- Configured an OCI uploader in `.ocmconfig` to enable localization during transfer
- Deployed the component by applying a `Bootstrap` composite, letting Crossplane compose the deployer objects directly
- Verified that localization kept image references in sync

This pattern lets developers ship deployment instructions alongside their software while operators only apply a simple composite, with Crossplane, rather than kro, reconciling the graph.

## Next Steps

- [Guides: Deploy a Helm Chart with kro and GitOps]({{< relref "/docs/guides/deploy/deploy-helm-chart-with-kro-and-gitops.md" >}}) - Drive the same delivery with kro and a Flux or Argo CD deployer
- [Guides: Transfer Components across an Air Gap]({{< relref "/docs/guides/transfer/air-gap-transfer.md" >}}) - Transfer components to disconnected environments
- [Guides: Configure Credentials for OCM Controllers]({{< relref "/docs/guides/deploy/configure-controller-credentials.md" >}}) - Set up private registry access

## Related Documentation

- [Concept: OCM Controllers]({{< relref "/docs/concepts/ocm-controllers.md" >}}) - Understand the controller architecture
