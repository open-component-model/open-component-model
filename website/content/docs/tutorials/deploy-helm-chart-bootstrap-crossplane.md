---
title: Deploy an Application from a Helm Chart with OCM and Crossplane
description: "Bootstrap a Helm chart deployment with OCM and Crossplane, using provider-kubernetes to create Flux objects, for repeatable localized installs."
icon: "⚙️"
weight: 62
toc: true
aliases:
  - /docs/tutorials/deploy-helm-chart-bootstrap-crossplane/
---

## What You'll Learn

This is the [Deploy an Application from a Helm Chart with OCM and kro]({{< relref "deploy-helm-chart-bootstrap.md" >}})
tutorial, rebuilt on **Crossplane** instead of kro. The goal is unchanged: one versioned OCM component that carries a
Helm chart, its container image, and the instructions to deploy it, with image references that localize themselves as
the component moves between registries. The only thing that changes is the graph engine. See
[How Crossplane fits in](#how-crossplane-fits-in) for the full mapping. [Podinfo](https://github.com/stefanprodan/podinfo)
stands in for your application.

By the end, you'll have:

- An OCM component containing a Helm chart, an image reference, and a Crossplane XRD + Composition
- A running Podinfo application deployed by claiming the `Bootstrap` composite
- Understanding of how localization keeps image references in sync after transfers

## Prerequisites

{{< callout context="note" title="Set up your environment" icon="outline/settings-check" >}}
Before starting, make sure you have set up your environment as described in the [setup guide]({{< relref "setup-controller-environment.md" >}}).
{{< /callout >}}

- [Controller environment]({{< relref "setup-controller-environment.md" >}}) with OCM Controllers installed
- [Crossplane](https://docs.crossplane.io/latest/get-started/install/) core, plus the
  [Kubernetes provider](https://github.com/crossplane-contrib/provider-kubernetes) (`provider-kubernetes`) with a
  `ProviderConfig` named `kubernetes-provider` using `InjectedIdentity`
- The [Patch and Transform function](https://github.com/crossplane-contrib/function-patch-and-transform)
  (`function-patch-and-transform`) installed, which the `Composition` runs as its pipeline step (installed below)
- Flux's `source-controller` and `helm-controller` (Crossplane creates the Flux objects, so kro and a GitOps deployer
  are not needed)
- [Custom RBAC]({{< relref "custom-rbac.md" >}}) letting the OCM controller manage `compositions` and
  `compositeresourcedefinitions` (`apiextensions.crossplane.io`)
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

The [bootstrap pattern]({{< relref "deploy-helm-chart-bootstrap.md" >}}) is unchanged from the kro variant: the
deployment graph is packaged inside the OCM component, and the Deployer controller extracts the blob and applies it.
The only difference is that the blob is a Crossplane XRD + Composition instead of a kro `ResourceGraphDefinition`.

### How Crossplane fits in

The difference from the kro variant is the **graph engine**, meaning what reconciles the deployment graph and creates
the Flux objects. Where the kro variant ships a `ResourceGraphDefinition` and lets kro register and reconcile a
`Bootstrap` CRD, this variant ships a Crossplane `CompositeResourceDefinition` (XRD) plus a `Composition`. Crossplane
offers a claimable `Bootstrap` kind, resolves the chart and image through OCM `Resource` objects, then uses the
[Crossplane Kubernetes provider](https://github.com/crossplane-contrib/provider-kubernetes) (`provider-kubernetes`)
to create the Flux `OCIRepository` and `HelmRelease`. Flux's `helm-controller` still renders the chart. Crossplane
simply replaces kro as the reconciler. The building blocks map like this:

| kro                                        | Crossplane                                                           |
| ------------------------------------------ | -------------------------------------------------------------------- |
| `ResourceGraphDefinition`                  | `CompositeResourceDefinition` (XRD) + `Composition`                  |
| Schema `kind: Bootstrap` registered by kro | Claim `kind: Bootstrap` offered by the XRD                           |
| Instance of the `Bootstrap` CRD            | Claim of the `Bootstrap` composite                                   |
| kro reconciles the graph                   | Crossplane reconciles the composite, creating the composed resources |

The `Composition` runs in **pipeline mode** (`mode: Pipeline`), the supported form since Crossplane v2 removed the
legacy inline `resources` field. A single pipeline step invokes the `function-patch-and-transform` function, whose
input holds the same patch-and-transform resource list the native engine used. The resolved chart and image OCI
coordinates travel from the OCM `Resource` objects to the Flux objects through the composite's `status.atProvider`
fields, since patch-and-transform wires values only between a composed resource and the composite, never directly
between two composed resources.

### Localization

[Localization]({{< relref "deploy-helm-chart-bootstrap.md" >}}) works exactly as in the kro variant: the transfer
updates image references in the descriptor, and at deploy time the **Composition** reads the resolved OCM `Resource`
status and injects the current image into the Helm values, so the workload always runs the image from the current
registry.

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
  - name: ocm.software/ocm-k8s-toolkit/bootstrap
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
status, where the two `Object` resources read them to build the Flux `OCIRepository` and `HelmRelease`.

```shell
cat > composition.yaml << 'EOF'
apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xbootstraps.ocm.software
spec:
  group: ocm.software
  names:
    kind: XBootstrap
    plural: xbootstraps
  claimNames:
    kind: Bootstrap
    plural: bootstraps
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
    kind: XBootstrap
  mode: Pipeline
  pipeline:
    - step: patch-and-transform
      functionRef:
        name: function-patch-and-transform
      input:
        apiVersion: pt.fn.crossplane.io/v1beta1
        kind: Resources
        resources:
          # 1. OCM Resource (chart): resolve the Helm chart OCI reference. The OCM
          #    Resource is namespace-scoped, and a Crossplane v1 Composition cannot
          #    compose a namespaced resource directly: the composite's
          #    spec.resourceRefs schema has only apiVersion/kind/name, so the engine
          #    fails with ".spec.resourceRefs[].namespace: field not declared in
          #    schema". Wrap the namespaced Resource inside a cluster-scoped
          #    provider-kubernetes Object (the pattern the OCM Deployer itself uses):
          #    the Object is what the composite references, and the namespaced
          #    Resource lives in spec.forProvider.manifest. additionalStatusFields/
          #    toOCI() exposes registry, repository, and digest under status.additional.
          - name: ocm-resource-chart
            base:
              apiVersion: kubernetes.crossplane.io/v1alpha2
              kind: Object
              spec:
                providerConfigRef:
                  name: kubernetes-provider
                forProvider:
                  manifest:
                    apiVersion: delivery.ocm.software/v1alpha1
                    kind: Resource
                    metadata:
                      name: placeholder  # overwritten by name patch below
                      namespace: default
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
                toFieldPath: spec.forProvider.manifest.spec.componentRef.name
              # Derive a claim-specific Resource name so parallel claims never
              # share one default/<name> OCM Resource.
              - type: FromCompositeFieldPath
                fromFieldPath: metadata.name
                toFieldPath: spec.forProvider.manifest.metadata.name
                transforms:
                  - type: string
                    string:
                      type: Format
                      fmt: "%s-chart"
              # Publish resolved chart coordinates onto composite status so the
              # OCIRepository Object below can consume them.
              - type: ToCompositeFieldPath
                fromFieldPath: status.atProvider.manifest.status.additional.oci.registry
                policy:
                  fromFieldPath: Optional
                toFieldPath: status.atProvider.chartRegistry
              - type: ToCompositeFieldPath
                fromFieldPath: status.atProvider.manifest.status.additional.oci.repository
                policy:
                  fromFieldPath: Optional
                toFieldPath: status.atProvider.chartRepository
              - type: ToCompositeFieldPath
                fromFieldPath: status.atProvider.manifest.status.additional.oci.digest
                policy:
                  fromFieldPath: Optional
                toFieldPath: status.atProvider.chartDigest
          # 2. OCM Resource (image): resolve the container image OCI reference for
          #    localization. Same Object-wrapping as the chart resource above.
          - name: ocm-resource-image
            base:
              apiVersion: kubernetes.crossplane.io/v1alpha2
              kind: Object
              spec:
                providerConfigRef:
                  name: kubernetes-provider
                forProvider:
                  manifest:
                    apiVersion: delivery.ocm.software/v1alpha1
                    kind: Resource
                    metadata:
                      name: placeholder  # overwritten by name patch below
                      namespace: default
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
                toFieldPath: spec.forProvider.manifest.spec.componentRef.name
              # Derive a claim-specific Resource name so parallel claims never
              # share one default/<name> OCM Resource.
              - type: FromCompositeFieldPath
                fromFieldPath: metadata.name
                toFieldPath: spec.forProvider.manifest.metadata.name
                transforms:
                  - type: string
                    string:
                      type: Format
                      fmt: "%s-image"
              - type: ToCompositeFieldPath
                fromFieldPath: status.atProvider.manifest.status.additional.oci.registry
                policy:
                  fromFieldPath: Optional
                toFieldPath: status.atProvider.imageRegistry
              - type: ToCompositeFieldPath
                fromFieldPath: status.atProvider.manifest.status.additional.oci.repository
                policy:
                  fromFieldPath: Optional
                toFieldPath: status.atProvider.imageRepository
              - type: ToCompositeFieldPath
                fromFieldPath: status.atProvider.manifest.status.additional.oci.digest
                policy:
                  fromFieldPath: Optional
                toFieldPath: status.atProvider.imageDigest
          # 3. Kubernetes Object: Flux OCIRepository, created directly by the
          #    Crossplane Kubernetes provider instead of by kro. The chart URL and
          #    digest come from the composite status populated in step 1.
          - name: ocirepository
            base:
              apiVersion: kubernetes.crossplane.io/v1alpha2
              kind: Object
              spec:
                providerConfigRef:
                  name: kubernetes-provider
                forProvider:
                  manifest:
                    apiVersion: source.toolkit.fluxcd.io/v1
                    kind: OCIRepository
                    metadata:
                      # The composite is cluster-scoped so its metadata.namespace is
                      # empty; set the Flux manifest namespace statically instead.
                      namespace: default
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
                toFieldPath: spec.forProvider.manifest.metadata.name
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
                toFieldPath: spec.forProvider.manifest.spec.url
              - type: FromCompositeFieldPath
                fromFieldPath: status.atProvider.chartDigest
                toFieldPath: spec.forProvider.manifest.spec.ref.digest
          # 4. Kubernetes Object: Flux HelmRelease, again created by the Crossplane
          #    Kubernetes provider. Localization happens here: the image repository and
          #    a pseudo-tag+digest are injected into the chart values from composite
          #    status, exactly as the kro RGD's helmrelease resource does.
          - name: helmrelease
            base:
              apiVersion: kubernetes.crossplane.io/v1alpha2
              kind: Object
              spec:
                providerConfigRef:
                  name: kubernetes-provider
                forProvider:
                  manifest:
                    apiVersion: helm.toolkit.fluxcd.io/v2
                    kind: HelmRelease
                    metadata:
                      # Static namespace: the cluster-scoped composite has no namespace.
                      namespace: default
                    spec:
                      releaseName: placeholder  # overwritten by patch below
                      interval: 1m
                      timeout: 5m
                      chartRef:
                        kind: OCIRepository
                        name: placeholder  # overwritten by patch below
                        namespace: default
            patches:
              - type: FromCompositeFieldPath
                fromFieldPath: metadata.name
                toFieldPath: spec.forProvider.manifest.metadata.name
                transforms:
                  - type: string
                    string:
                      type: Format
                      fmt: "%s-helmrelease"
              # Derive a claim-specific, reader-deterministic Helm release name from
              # the claim name so parallel claims never share one Helm release.
              - type: FromCompositeFieldPath
                fromFieldPath: spec.claimRef.name
                toFieldPath: spec.forProvider.manifest.spec.releaseName
                transforms:
                  - type: string
                    string:
                      type: Format
                      fmt: "%s-release"
              # Point the HelmRelease at the OCIRepository created in step 3.
              - type: FromCompositeFieldPath
                fromFieldPath: metadata.name
                toFieldPath: spec.forProvider.manifest.spec.chartRef.name
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
                toFieldPath: spec.forProvider.manifest.spec.values.image.repository
              # Pseudo-tag with @digest. Podinfo's chart builds repository:tag, and OCI
              # runtimes ignore the tag when a digest is present, so this pins by digest.
              - type: FromCompositeFieldPath
                fromFieldPath: status.atProvider.imageDigest
                toFieldPath: spec.forProvider.manifest.spec.values.image.tag
                transforms:
                  - type: string
                    string:
                      type: Format
                      fmt: "latest@%s"
EOF
```

{{< /details >}}

Make the component public in the GitHub `packages` tab by opening the
`component-descriptors/ocm.software/ocm-k8s-toolkit/bootstrap` package and setting its visibility to `public`.
Alternatively, keep it private and configure credentials for the OCM Controllers and Flux before `ocm add cv`. See
[Credentials for OCM Controllers]({{< relref "/docs/how-to/configure-credentials-ocm-controllers.md" >}}),
and add a `secretRef` to the `OCIRepository` manifest in the Composition's `ocirepository` resource.
{{< /step >}}

{{< step >}}

### Build and transfer the component

Build the component version locally:

```bash
ocm add cv
```

Transfer to your registry with `--copy-resources --upload-as ociArtifact` to enable localization. The `--upload-as ociArtifact` flag is required so the Helm chart and image land as OCI artifacts in the target registry, keeping image references the Composition can rewrite:

```bash
ocm transfer cv --copy-resources --upload-as ociArtifact transport-archive//ocm.software/ocm-k8s-toolkit/bootstrap:1.0.0 $OCM_REPO
```

{{< /step >}}

{{< step >}}

### Verify the transfer

Check that the component was transferred and resources were localized:

```bash
ocm get cv $OCM_REPO//ocm.software/ocm-k8s-toolkit/bootstrap:1.0.0 -o yaml | grep imageReference
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

### Install the Patch and Transform function

The `Composition` runs in pipeline mode and references `function-patch-and-transform`, so install it before the
Deployer applies the Composition:

```bash
cat << 'EOF' | kubectl apply -f -
apiVersion: pkg.crossplane.io/v1
kind: Function
metadata:
  name: function-patch-and-transform
spec:
  package: xpkg.crossplane.io/crossplane-contrib/function-patch-and-transform:v0.8.2
EOF
```

Wait for the function to become healthy:

```bash
kubectl get function.pkg function-patch-and-transform -w
```

{{< /step >}}

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
  component: ocm.software/ocm-k8s-toolkit/bootstrap
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

Wait for the Composition and its XRD to be installed (this may take 30-60 seconds):

```bash
kubectl get xrd,composition -w
```

<details>
<summary>You should see this output</summary>

```console
NAME                                                     ESTABLISHED   OFFERED   AGE
compositeresourcedefinition.../xbootstraps.ocm.software  True          True      2m56s

NAME                                    XR-KIND      XR-APIVERSION           AGE
composition.apiextensions.../bootstrap  XBootstrap   ocm.software/v1alpha1   2m56s
```

</details>

When the XRD is `ESTABLISHED`/`OFFERED`, the Deployer has applied the blob and Crossplane has registered the
claimable `Bootstrap` kind.
{{< /step >}}

{{< step >}}

### Create a claim

Now create a **claim** of the `Bootstrap` composite to trigger the actual deployment. `ocmComponent` selects the OCM
`Component` CR created by the bootstrap resources (`bootstrap-component`), matching the XRD's default. Create
`instance.yaml`:

```shell
cat > instance.yaml << 'EOF'
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

Apply the claim to the cluster:

```bash
kubectl apply -f instance.yaml
```

<details>
<summary>You should see this output</summary>

```console
bootstrap.ocm.software/bootstrap created
```

</details>

Wait for the claim and its composed resources to become ready:

```bash
kubectl get bootstrap.ocm.software -w
```

<details>
<summary>You should see this output</summary>

```console
NAME        SYNCED   READY   CONNECTION-SECRET   AGE
bootstrap   True     True                        3m23s
```

</details>

When `SYNCED` and `READY` are both `True`, Crossplane has resolved the OCM resources and the Kubernetes provider has
created the Flux `OCIRepository` and `HelmRelease`, which Flux then reconciles into the running workload.
{{< /step >}}

{{< step >}}

### Verify localization

Check that the deployed pod uses the localized image from your registry (not the original `ghcr.io/stefanprodan/...`).
The claim can report `READY` before Flux has reconciled the chart into a running pod, so wait for the pod to appear
first:

```bash
kubectl wait --for=condition=Ready pod -l app.kubernetes.io/name=bootstrap-release-podinfo --timeout=300s
kubectl get pods -l app.kubernetes.io/name=bootstrap-release-podinfo -o jsonpath='{.items[0].spec.containers[0].image}'
```

<details>
<summary>You should see this output</summary>

```console
ghcr.io/$GITHUB_USERNAME/component-descriptors/ocm.software/ocm-k8s-toolkit/bootstrap:latest@sha256:262578cde928d5c9eba3bce079976444f624c13ed0afb741d90d5423877496cb
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
Composition (`apiextensions.crossplane.io`), the `Bootstrap` claim (`ocm.software`), and the OCM `Resource`
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

Separately, if the claim's composed `Object` resources stay stuck (see [Claim Not Becoming
Ready](#claim-not-becoming-ready)), the fault is the Kubernetes provider's `ServiceAccount`, not the OCM
controller. That account needs RBAC for the Flux objects it creates
(`ocirepositories.source.toolkit.fluxcd.io`, `helmreleases.helm.toolkit.fluxcd.io`). The dev-friendly install
grants this broadly. On a hardened cluster, bind an equivalent `ClusterRole` to the provider's service account.

#### Composed OCM `Resource` objects stay forbidden

The two OCM `Resource` objects (the chart and the image) are applied by the Kubernetes provider, not by Crossplane
core. If the provider's service account (`provider-kubernetes`) lacks the grant, the composed `Object` reports a
`forbidden` error on its status, for example

```text
cannot apply object: resources.delivery.ocm.software "<xr>-image" is forbidden: User
"system:serviceaccount:crossplane-system:provider-kubernetes" cannot patch resource
"resources" in API group "delivery.ocm.software" at the cluster scope
```

The dev-friendly install grants this broadly. On a hardened cluster, bind a `ClusterRole` for the OCM `Resource`
objects to the provider's service account:

```bash
cat << 'EOF' | kubectl apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: provider-kubernetes-ocm-resources
rules:
  - apiGroups: ["delivery.ocm.software"]
    resources: ["resources", "resources/status"]
    verbs: ["create", "delete", "get", "list", "patch", "update", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: provider-kubernetes-ocm-resources
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: provider-kubernetes-ocm-resources
subjects:
  - kind: ServiceAccount
    name: provider-kubernetes
    namespace: crossplane-system
EOF
```

The provider's service account name is the one on its running pod. Look yours up with
`kubectl get pod -n crossplane-system -l pkg.crossplane.io/provider=provider-kubernetes -o jsonpath='{.items[0].spec.serviceAccountName}'`.
Confirm the grant propagated (Crossplane re-reconciles the composite within about a minute):

```bash
kubectl auth can-i patch resources.delivery.ocm.software \
  --as=system:serviceaccount:crossplane-system:provider-kubernetes
```

### Claim Not Becoming Ready

If the `Bootstrap` claim stays `SYNCED=False` or `READY=False`:

```bash
kubectl describe bootstrap.ocm.software bootstrap
```

Inspect the composed resources directly to see which one is stuck:

```bash
kubectl get object,resource.delivery.ocm.software
kubectl describe object <name>-helmrelease
```

Check the Events section and the composed `Object` status for error messages. A common cause is the Kubernetes
provider's `ProviderConfig` (`kubernetes-provider`) missing or lacking RBAC for the Flux CRDs.

#### `.spec.resourceRefs[].namespace: field not declared in schema`

If the composite (`XBootstrap`) stays `SYNCED=False` with an error like `cannot compose resources: cannot update
composite resource spec.resourceRefs: ... .spec.resourceRefs[0].namespace: field not declared in schema` (or the
Crossplane logs show `an empty namespace may not be set when a resource name is provided`), you have hit a Crossplane
v1 limitation: **a Composition cannot compose a namespace-scoped resource directly.** The composite records every
composed resource in `spec.resourceRefs`, whose schema carries only `apiVersion`, `kind`, and `name`. When the
composed resource is namespaced, the engine tries to persist its namespace into that ref and the write is rejected,
so the resource is never created.

The OCM `Resource` objects (`delivery.ocm.software`) are namespace-scoped, so they cannot be listed directly under
`resources:`. The fix this tutorial uses is to **wrap each namespaced `Resource` in a cluster-scoped
provider-kubernetes `Object`** (the same mechanism the OCM Deployer uses): the composite references only the
cluster-scoped `Object`s, while the namespaced `Resource` lives in `spec.forProvider.manifest` and the provider
applies it. Read its resolved coordinates back through `status.atProvider.manifest.status.additional.*`. If you ever
place a namespaced kind directly under `resources:`, this error returns.

### ImagePullBackOff Errors

If pods show `ImagePullBackOff` or `ErrImagePull` errors, the kubelet cannot pull the localized image from your private registry. Add `imagePullSecrets` to the HelmRelease values in the Composition's `helmrelease` resource.

## What You Learned

You've successfully:

- Created an OCM component with an embedded Crossplane XRD + Composition
- Used `--copy-resources --upload-as ociArtifact` to enable localization during transfer
- Deployed the component by claiming a `Bootstrap` composite, letting Crossplane's Kubernetes provider create the Flux objects
- Verified that localization kept image references in sync

This pattern lets developers ship deployment instructions alongside their software while operators only claim a simple composite, with Crossplane, rather than kro, reconciling the graph.

## Next Steps

- [Deploy an Application from a Helm Chart with OCM and kro]({{< relref "deploy-helm-chart-bootstrap.md" >}}) covers the same delivery driven by kro with a Flux or Argo CD deployer
- [How-to: Air-Gap Transfer]({{< relref "air-gap-transfer.md" >}}) transfers components to disconnected environments
- [How-to: Configure Credentials for Controllers]({{< relref "docs/how-to/configure-credentials-ocm-controllers.md" >}}) sets up private registry access
- [Concept: OCM Controllers]({{< relref "ocm-controllers.md" >}}) explains the controller architecture
