---
title: Deploy an Application from a Helm Chart with OCM and Crossplane
description: "Bootstrap a Helm chart deployment with OCM and Crossplane, composing Flux objects directly from an XRD and Composition for repeatable localized installs."
icon: "⚙️"
weight: 62
toc: true
aliases:
  - /docs/tutorials/deploy-helm-chart-bootstrap-crossplane/
---

## What You'll Learn

Your application ships as a Helm chart, and you want its delivery to be repeatable: one versioned OCM component that
carries the chart, its container image, and the instructions to deploy it, with image references that localize
themselves as the component moves between registries. This tutorial builds that deployment on **Crossplane**, using
a `CompositeResourceDefinition` (XRD) plus a `Composition` to reconcile the deployment graph. The XRD composes the
Flux objects directly. We will use [Podinfo](https://github.com/stefanprodan/podinfo) as the example application.

{{< callout context="note" title="Coming from the kro tutorial?" icon="outline/info-circle" >}}
This tutorial reaches the same end state as [Deploy an Application from a Helm Chart with OCM and
kro]({{< relref "deploy-helm-chart-bootstrap.md" >}}), swapping kro's `ResourceGraphDefinition` for a Crossplane
XRD + Composition. Reading the kro tutorial first is **not** required.
{{< /callout >}}

By the end, you'll have:

- An OCM component containing a Helm chart, an image reference, and a Crossplane XRD + Composition
- A running Podinfo application deployed by applying the `Bootstrap` composite resource
- Understanding of how localization keeps image references in sync after transfers

## Prerequisites

{{< callout context="note" title="Set up your environment" icon="outline/settings-check" >}}
Before starting, make sure you have set up your environment as described in the [setup guide]({{< relref "setup-controller-environment.md" >}}).
{{< /callout >}}

- [Controller environment]({{< relref "setup-controller-environment.md" >}}) with OCM Controllers installed
- **Crossplane** installed as the orchestrator, following [Set up Controller Environments > Install an
  Orchestrator]({{< relref "setup-controller-environment.md#install-an-orchestrator" >}}) (the Crossplane tab)
- **Flux** installed as the deployer, following [Set up Controller Environments > Install a
  Deployer]({{< relref "setup-controller-environment.md#install-a-deployer" >}}) (the Flux tab). Crossplane creates the
  Flux `OCIRepository` and `HelmRelease` objects, so Flux's `source-controller` and `helm-controller` must be running.
  kro and a GitOps deployer are not needed.
- [Custom RBAC]({{< relref "custom-rbac.md" >}}) letting the OCM controller manage `compositions` and
  `compositeresourcedefinitions` (`apiextensions.crossplane.io`), plus a grant letting Crossplane compose the Flux
  objects; see the Crossplane tab in that guide
- The [Patch and Transform function](https://github.com/crossplane-contrib/function-patch-and-transform)
  (`function-patch-and-transform`), which the `Composition` runs as its pipeline step. Install it via the Crossplane
  tab of [Set up Controller Environments > Install an Orchestrator]({{< relref "setup-controller-environment.md#install-an-orchestrator" >}}).
- [OCM CLI]({{< relref "ocm-cli-installation.md" >}}) installed
- Access to an OCI registry (e.g., [ghcr.io](https://docs.github.com/en/packages/learn-github-packages/introduction-to-github-packages))
- `envsubst` installed (pre-installed on most Linux/macOS systems as part of `gettext`)

{{< callout context="note" title="Private registries" icon="outline/lock" >}}
If using a private registry, you'll need to configure credentials for both the OCM CLI and the controller resources. See [Configure Credentials for Controllers]({{< relref "docs/how-to/configure-credentials-ocm-controllers.md" >}}) for details.
{{< /callout >}}

## Environment Setup

Set environment variables for your GitHub username and OCM repository:

```bash
export GITHUB_USERNAME=<your-github-username>
export OCM_REPO=ghcr.io/$GITHUB_USERNAME/ocm-tutorial
```

## Concepts

### The Bootstrap Pattern

Instead of applying the deployment graph (the XRD + Composition) to the cluster by hand, the **bootstrap pattern**
packages that graph as a resource *inside* the OCM component. The OCM Deployer controller then extracts the blob from
the component and applies it for you. The deployment instructions travel with the component and stay versioned
alongside the chart and image, so every environment installs the same graph. In this tutorial the packaged blob is a
Crossplane XRD + Composition.

### How the Pieces Fit Together

The **graph engine** is what reconciles the deployment graph and creates the Flux objects. This tutorial ships a
Crossplane `CompositeResourceDefinition` (XRD) plus a `Composition`. The XRD registers a `Bootstrap` composite
kind. When you apply a `Bootstrap` resource, Crossplane resolves the chart and image through OCM `Resource` objects,
then composes the Flux `OCIRepository` and `HelmRelease` directly. Flux's `helm-controller` renders the chart into the
running workload.

The OCM `Resource` and the Flux objects are composed directly by the Composition, with no intermediate wrapper.

If you already know the kro variant, the building blocks map like this (otherwise skip the table):

| kro                                        | Crossplane                                                           |
| ------------------------------------------ | -------------------------------------------------------------------- |
| `ResourceGraphDefinition`                  | `CompositeResourceDefinition` (XRD) + `Composition`                  |
| Schema `kind: Bootstrap` registered by kro | Composite `kind: Bootstrap` registered by the XRD                    |
| Instance of the `Bootstrap` CRD            | A `Bootstrap` composite resource                                     |
| kro reconciles the graph                   | Crossplane reconciles the composite, creating the composed resources |

The `Composition` runs in **pipeline mode** (`mode: Pipeline`). A single pipeline step invokes the `function-patch-and-transform` function, whose
input holds the patch-and-transform resource list. The resolved chart and image OCI coordinates travel from the OCM
`Resource` objects to the Flux objects through the composite's `status.atProvider` fields, since patch-and-transform
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
this variant that file holds a Crossplane XRD + Composition. Create it before building the component version:

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
  name: xbootstraps.ocm.software
spec:
  group: ocm.software
  names:
    kind: Bootstrap
    plural: bootstraps
  # Namespaced keeps the composite and its composed resources in one namespace,
  # the same one you apply the Bootstrap into. You apply the Bootstrap directly.
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
    crossplane.io/xrd: xbootstraps.ocm.software
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
          #    and digest come from the composite status populated in step 1.
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
                toFieldPath: spec.url
              - type: FromCompositeFieldPath
                fromFieldPath: status.atProvider.chartDigest
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
                toFieldPath: spec.values.image.repository
              # Pseudo-tag with @digest. Podinfo's chart builds repository:tag, and OCI
              # runtimes ignore the tag when a digest is present, so this pins by digest.
              - type: FromCompositeFieldPath
                fromFieldPath: status.atProvider.imageDigest
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
[Credentials for OCM Controllers]({{< relref "/docs/how-to/configure-credentials-ocm-controllers.md" >}}), and add a
`secretRef` to the `OCIRepository` manifest in the Composition's `ocirepository` resource.
{{< /callout >}}
{{< /step >}}

{{< step >}}

### Build and transfer the component

Build the component version locally:

```bash
ocm add cv
```

Transfer to your registry with `--copy-resources --upload-as ociArtifact` to enable localization. The `--upload-as ociArtifact` flag is required so the Helm chart and image land as OCI artifacts in the target registry, keeping image references the Composition can rewrite:

```bash
ocm transfer cv --copy-resources --upload-as ociArtifact transport-archive//ocm.software/ocm-k8s-toolkit/bootstrap-crossplane:1.0.0 $OCM_REPO
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
identical to the kro variant. The Deployer applies whatever blob the component carries, so nothing here is
Crossplane-specific.

{{< steps >}}
{{< step >}}

### Create bootstrap resources

Create `bootstrap.yaml`:

{{< callout context="note" title="Private registries" icon="outline/lock" >}}
If you chose to keep your package private, do not forget to add the
secret to the repository's `ocmConfig`, as described above!
{{< /callout >}}

{{< details "Bootstrap Resources (bootstrap.yaml)" >}}

```shell
cat > bootstrap.yaml << 'EOF'
apiVersion: delivery.ocm.software/v1alpha1
kind: Repository
metadata:
  name: bootstrap-repository
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
  namespace: default
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
apiVersion: delivery.ocm.software/v1alpha1
kind: Deployer
metadata:
  name: bootstrap-deployer
spec:
  resourceRef:
    # Reference to the OCM resource that contains the XRD + Composition blob.
    name: bootstrap-composition
    # Crossplane XRDs and Compositions are cluster-scoped, so the deployer is
    # cluster-scoped too. Set the namespace of the referenced resource here.
    namespace: default
  # ocmConfig is required, if the OCM repository requires credentials to access it.
  # (You also need to specify the namespace of the reference as the 'deployer' is cluster-scoped.)
  # ocmConfig:
EOF
```

{{< /details >}}
{{< /step >}}

{{< step >}}

### Apply the bootstrap resources

{{< callout context="caution" title="RBAC required before you apply" icon="outline/alert-triangle" >}}
Please make sure that you updated your RBAC permissions before applying this command. Follow our [Configure Custom RBAC for Deployers]({{< relref "custom-rbac.md" >}}) guide to know how to do that.
{{< /callout >}}

```bash
envsubst < bootstrap.yaml | kubectl apply -f -
```

Wait for the XRD to be installed (this may take 30-60 seconds). `kubectl` rejects `-w` with more than one resource
type, so watch the XRD on its own:

```bash
kubectl get xrd -w
```

Once the XRD is `ESTABLISHED`/`OFFERED`, check the Composition:

```bash
kubectl get composition
```

<details>
<summary>You should see this output</summary>

```console
NAME                                                     ESTABLISHED   OFFERED   AGE
compositeresourcedefinition.../xbootstraps.ocm.software  True          True      2m56s

NAME                                    XR-KIND     XR-APIVERSION           AGE
composition.apiextensions.../bootstrap  Bootstrap   ocm.software/v1alpha1   2m56s
```

</details>

When the XRD is `ESTABLISHED`/`OFFERED`, the Deployer has applied the blob and Crossplane has registered the
`Bootstrap` composite kind.
{{< /step >}}

{{< step >}}

### Create the Bootstrap resource

Now create a `Bootstrap` composite resource to trigger the actual deployment. You apply the composite kind directly.
The composite and its composed resources live in the namespace you apply it into (`default` here).
`ocmComponent` selects the OCM `Component` CR created by the bootstrap resources
(`bootstrap-component`), matching the XRD's default. Create `bootstrap-xr.yaml`:

```shell
cat > bootstrap-xr.yaml << 'EOF'
apiVersion: ocm.software/v1alpha1
kind: Bootstrap
metadata:
  name: bootstrap
spec:
  ocmComponent: bootstrap-component
EOF
```

{{< /step >}}

{{< step >}}

### Deploy the application

Apply the composite to the cluster:

```bash
kubectl apply -f bootstrap-xr.yaml
```

<details>
<summary>You should see this output</summary>

```console
bootstrap.ocm.software/bootstrap created
```

</details>

Wait for the composite and its composed resources to become ready:

```bash
kubectl get bootstrap.ocm.software -w
```

<details>
<summary>You should see this output</summary>

```console
NAME        SYNCED   READY   COMPOSITION   AGE
bootstrap   True     True    bootstrap     3m23s
```

</details>

When `SYNCED` and `READY` are both `True`, Crossplane has resolved the OCM resources and composed the Flux
`OCIRepository` and `HelmRelease` directly, which Flux then reconciles into the running workload.
{{< /step >}}

{{< step >}}

### Verify localization

Check that the deployed pod uses the localized image from your registry (not the original `ghcr.io/stefanprodan/...`).
The composite can report `READY` before Flux has reconciled the chart into a running pod, so wait for the pod to
appear first:

```bash
kubectl wait --for=condition=Ready pod -l app.kubernetes.io/name=bootstrap-release-podinfo --timeout=300s
kubectl get pods -l app.kubernetes.io/name=bootstrap-release-podinfo -o jsonpath='{.items[0].spec.containers[0].image}'
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

Common causes: missing component, wrong repository URL, credential issues, or the OCM controller lacking RBAC for
`compositions`/`compositeresourcedefinitions`.

### RBAC Permission Errors

If the controller logs show permission errors like `forbidden` or `cannot create resource`, the controller's
`ServiceAccount` lacks RBAC for the Crossplane objects the Deployer applies. The
[Custom RBAC guide]({{< relref "custom-rbac.md" >}}) covers the general pattern (its examples target kro, so the
API groups differ). For this Crossplane variant, grant the groups the Deployer actually touches: the XRD and
Composition (`apiextensions.crossplane.io`), the `Bootstrap` composite (`ocm.software`), and the OCM `Resource`
objects (`delivery.ocm.software`).

{{< callout context="note" title="Service account name" icon="outline/info-circle" >}}
The binding below assumes the controller `ServiceAccount` is `ocm-k8s-toolkit-controller-manager` in
`ocm-k8s-toolkit-system`. Other Helm release names produce different names. Look yours up with
`kubectl get sa -n ocm-k8s-toolkit-system -l app.kubernetes.io/name=ocm-k8s-toolkit`.
{{< /callout >}}

```bash
cat << 'EOF' | kubectl apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: ocm-controller-crossplane
rules:
  - apiGroups: ["apiextensions.crossplane.io"]
    resources: ["compositeresourcedefinitions", "compositions"]
    verbs: ["create", "delete", "get", "list", "patch", "update", "watch"]
  - apiGroups: ["ocm.software"]
    resources: ["bootstraps"]
    verbs: ["create", "delete", "get", "list", "patch", "update", "watch"]
  - apiGroups: ["delivery.ocm.software"]
    resources: ["resources"]
    verbs: ["create", "delete", "get", "list", "patch", "update", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ocm-controller-crossplane
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: ocm-controller-crossplane
subjects:
  - kind: ServiceAccount
    name: ocm-k8s-toolkit-controller-manager
    namespace: ocm-k8s-toolkit-system
EOF
```

Confirm the grant took effect:

```bash
kubectl auth can-i create compositions.apiextensions.crossplane.io \
  --as=system:serviceaccount:ocm-k8s-toolkit-system:ocm-k8s-toolkit-controller-manager
```

Separately, if the composite's composed resources stay stuck (see [Composite Not Becoming
Ready](#composite-not-becoming-ready)), the fault is Crossplane's own service account. Crossplane composes the
OCM `Resource` and Flux objects directly, so Crossplane core must be granted access to those non-Crossplane kinds.
See the next section.

#### Composed resources stay forbidden

Crossplane's service account can create any resource installed by a provider or defined by an XRD, but it must be
granted access to compose any other kind of resource (the OCM `Resource` and the Flux objects here). Without that
grant the composite reports a `forbidden` error, for example

```text
cannot compose resources: ... resources.delivery.ocm.software "<xr>-image" is forbidden: User
"system:serviceaccount:crossplane-system:crossplane" cannot create resource
"resources" in API group "delivery.ocm.software" in the namespace "default"
```

Grant Crossplane's service account access to the composed kinds with an aggregated `ClusterRole`, as described in
[Configure Custom RBAC for Deployers > Crossplane]({{< relref "/docs/how-to/custom-rbac.md#crossplane" >}}).

### Composite Not Becoming Ready

If the `Bootstrap` composite stays `SYNCED=False` or `READY=False`:

```bash
kubectl describe bootstrap.ocm.software bootstrap
```

Inspect the composed resources directly to see which one is stuck:

```bash
kubectl get resource.delivery.ocm.software,ocirepository,helmrelease
kubectl describe helmrelease bootstrap-helmrelease
```

Check the Events section and each composed resource's status for error messages. A common cause is Crossplane
lacking RBAC for the composed OCM `Resource` or Flux CRDs (see [Composed resources stay
forbidden](#composed-resources-stay-forbidden)).

### ImagePullBackOff Errors

If pods show `ImagePullBackOff` or `ErrImagePull` errors, the kubelet cannot pull the localized image from your private registry. Add `imagePullSecrets` to the HelmRelease values in the Composition's `helmrelease` resource.

## What You Learned

You've successfully:

- Created an OCM component with an embedded Crossplane XRD + Composition
- Used `--copy-resources --upload-as ociArtifact` to enable localization during transfer
- Deployed the component by applying a `Bootstrap` composite, letting Crossplane compose the Flux objects directly
- Verified that localization kept image references in sync

This pattern lets developers ship deployment instructions alongside their software while operators only apply a simple composite, with Crossplane, rather than kro, reconciling the graph.

## Next Steps

- [Deploy an Application from a Helm Chart with OCM and kro]({{< relref "deploy-helm-chart-bootstrap.md" >}}) covers the same delivery driven by kro with a Flux or Argo CD deployer
- [How-to: Air-Gap Transfer]({{< relref "air-gap-transfer.md" >}}) transfers components to disconnected environments
- [How-to: Configure Credentials for Controllers]({{< relref "docs/how-to/configure-credentials-ocm-controllers.md" >}}) sets up private registry access
- [Concept: OCM Controllers]({{< relref "ocm-controllers.md" >}}) explains the controller architecture
