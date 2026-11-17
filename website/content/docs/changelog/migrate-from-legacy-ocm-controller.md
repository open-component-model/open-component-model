---
title: "Migrate from the Legacy OCM Controller"
description: "Replace the legacy ocm-controller and its FluxDeployer objects with the new OCM controllers without deleting running workloads."
icon: "arrow-autofit-right"
slug: "migrate-from-legacy-ocm-controller"
weight: 100
toc: true
---

## Goal

Move from the legacy [ocm-controller](https://github.com/open-component-model/ocm-controller) to the new OCM
controllers. The workloads that a `FluxDeployer` deployed keep running during the move.

{{< callout context="caution" title="The controllers cannot share a cluster" icon="outline/alert-triangle" >}}
Both controllers use the API group `delivery.ocm.software` with version `v1alpha1`, and both define a kind `Resource`
with a different schema. A cluster serves only one of them. While the legacy CRDs exist, `helm install` of the new
controllers fails. If you apply the new CRDs over the legacy ones, legacy `Resource` objects show an almost empty
`spec`, and the next write to them deletes their legacy fields for good. Remove the legacy objects, controller and
CRDs first, then install the new controllers. To run both at the same time, use a second cluster for the new ones.
{{< /callout >}}

## Why Migrate?

- The README of the legacy `ocm-controller` points to the new controllers in `open-component-model/open-component-model`.
- The new controllers need no in-cluster OCI registry and no `Snapshot` objects. They fetch resources on demand.
- A `NamespacedDeployer` applies plain manifests with the permissions of a service account. Flux is only needed for
  Helm charts and Kustomize features.

For all changes between OCM v1 and v2, see
[Key Differences Between OCM v1 and v2]({{< docref "docs/changelog/differences-v1-v2.md" >}}).

## Prerequisites

- `kubectl`, [Helm](https://helm.sh/docs/intro/install/) and `jq` installed
- Cluster admin permissions, as you delete CRDs and cluster roles
- Flux keeps running during the migration. It removes the finalizers of the Flux objects you delete, and it keeps
  deploying your Helm charts.
- A maintenance window. From step 3 until the new objects are `Ready`, no new component version rolls out. Running
  workloads are not touched.

The examples use the namespace `my-team`, the component `ocm.software/podinfo` in `ghcr.io/my-org/components`, a
`FluxDeployer` named `podinfo` for plain manifests and one named `podinfo-chart` for a Helm chart.

## Steps

### 1. Translate the legacy objects

Save each **After** example to the file named in its tab. You apply the files in step 8. Create the new objects in
the namespace the `FluxDeployer` deployed into, its `targetNamespace`. New objects reference their parents in the
same namespace only, and a `NamespacedDeployer` applies objects without a namespace into its own namespace.

| Legacy kind | New kind | Notes |
| ----------- | -------- | ----- |
| `ComponentVersion` | `Repository` and `Component` | `spec.version.semver` becomes `spec.semver` |
| `ComponentVersion` with `spec.destination` | `Repository` and `Replication` | |
| `ComponentDescriptor` | None | The descriptor data is in the `Component` status |
| `Resource` | `Resource` | New schema. `sourceRef.resourceRef.referencePath` becomes `resource.byReference.referencePath` |
| `Snapshot` | None | No in-cluster registry |
| `FluxDeployer` with `kustomizationTemplate` | `NamespacedDeployer` | Applies manifests as they are |
| `FluxDeployer` with `helmReleaseTemplate` | Flux `OCIRepository` and `HelmRelease` | The `Resource` status publishes the chart location |
| `Configuration`, `Localization` | None | See the end of this step |

**Credentials.** A Secret with a `.dockerconfigjson` key works unchanged. Rename an `.ocmcredentialconfig` key to
`.ocmconfig`. Replace a Secret with plain `username` and `password` keys by one from
`kubectl create secret docker-registry`. There is no `serviceAccountName`: reference its image pull Secret in
`ocmConfig` instead.

**Verification.** `spec.verify` moves to an OCM configuration in a Secret. The legacy key Secret stores the public key
under the signature name:

```bash
kubectl get secret signing-keys -n my-team -o jsonpath='{.data.ocm\.software}' | base64 -d
```

Set `algorithm` to the algorithm of the signature. OCM v1 signs with `RSASSA-PKCS1-V1_5` by default. See
[Verify Component Signatures on Deployment]({{< docref "docs/guides/deploy/verify-component-signatures-on-deployment.md" >}}).

{{< tabs "componentversion" >}}
{{< tab "Before" >}}

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: ComponentVersion
metadata:
  name: podinfo
  namespace: my-team
spec:
  interval: 10m0s
  component: ocm.software/podinfo
  version:
    semver: ">=6.2.3"
  repository:
    url: ghcr.io/my-org/components
    secretRef:
      name: registry-credentials
  verify:
    - name: ocm.software
      publicKey:
        secretRef:
          name: signing-keys
```

{{< /tab >}}
{{< tab "After: component.yaml" >}}

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Repository
metadata:
  name: podinfo
  namespace: my-team
spec:
  repositorySpec:
    type: OCIRegistry
    baseUrl: ghcr.io/my-org/components
  ocmConfig:
    - kind: Secret
      name: registry-credentials
  interval: 10m
---
apiVersion: v1
kind: Secret
metadata:
  name: ocm-verification
  namespace: my-team
stringData:
  .ocmconfig: |
    type: generic.config.ocm.software/v1
    configurations:
    - type: signing.config.ocm.software/v1alpha1
      signature: ocm.software
    - type: credentials.config.ocm.software
      consumers:
      - identity:
          type: RSA/v1alpha1
          algorithm: RSASSA-PKCS1-V1_5
          signature: ocm.software
        credentials:
        - type: Credentials/v1
          properties:
            publicKeyPEM: |
              -----BEGIN PUBLIC KEY-----
              <PEM public key>
              -----END PUBLIC KEY-----
---
apiVersion: delivery.ocm.software/v1alpha1
kind: Component
metadata:
  name: podinfo
  namespace: my-team
spec:
  repositoryRef:
    name: podinfo
  component: ocm.software/podinfo
  semver: ">=6.2.3"
  interval: 10m
  ocmConfig:
    # An own ocmConfig replaces the inherited one, so reference the Repository to keep its credentials.
    - kind: Repository
      apiVersion: delivery.ocm.software/v1alpha1
      name: podinfo
    - kind: Secret
      name: ocm-verification
```

{{< /tab >}}
{{< /tabs >}}

**Destination.** With `spec.destination`, the legacy controller transferred the component version recursively with
all resources, and read resources from the target afterwards. A `Replication` does the transfer. The `ConfigMap`
below copies all resources, as before, and the target `Repository` supplies the target credentials. To read resources
from the target, add a second `Component` that references the target `Repository`. See
[Replicate Component Versions with the Controller]({{< docref "docs/guides/transfer/replicate-component-versions.md" >}}).

{{< tabs "destination" >}}
{{< tab "Before" >}}

```yaml
spec:
  destination:
    url: registry.example.com/mirror
    secretRef:
      name: mirror-credentials
```

{{< /tab >}}
{{< tab "After: replication.yaml" >}}

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: transfer-config
  namespace: my-team
data:
  .ocmconfig: |
    type: generic.config.ocm.software/v1
    configurations:
    - type: transfer.config.ocm.software/v1alpha1
      recursive: -1
    - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
---
apiVersion: delivery.ocm.software/v1alpha1
kind: Repository
metadata:
  name: mirror
  namespace: my-team
spec:
  repositorySpec:
    type: OCIRegistry
    baseUrl: registry.example.com/mirror
  ocmConfig:
    - kind: Secret
      name: mirror-credentials
  interval: 10m
---
apiVersion: delivery.ocm.software/v1alpha1
kind: Replication
metadata:
  name: podinfo-mirror
  namespace: my-team
spec:
  componentRef:
    name: podinfo
  targetRepositoryRef:
    name: mirror
  ocmConfig:
    - kind: ConfigMap
      name: transfer-config
    - kind: Component
      apiVersion: delivery.ocm.software/v1alpha1
      name: podinfo
```

{{< /tab >}}
{{< /tabs >}}

**Resource.** The new `Resource` has no `interval`, it follows its `Component`. For a Helm chart, create a second
`Resource` named `podinfo-chart` for the chart and add `additionalStatusFields` with `oci: resource.access.toOCI()`,
so that it publishes the chart location.

{{< tabs "resource" >}}
{{< tab "Before" >}}

```yaml
spec:
  interval: 10m0s
  sourceRef:
    kind: ComponentVersion
    name: podinfo
    resourceRef:
      name: manifests
```

{{< /tab >}}
{{< tab "After: resource.yaml" >}}

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Resource
metadata:
  name: podinfo-manifests
  namespace: my-team
spec:
  componentRef:
    name: podinfo
  resource:
    byReference:
      resource:
        name: manifests
```

{{< /tab >}}
{{< /tabs >}}

**FluxDeployer.** A `kustomizationTemplate` becomes a `NamespacedDeployer`, which applies with the service account
you create in step 9. If the template uses `patches`, `images` or `postBuild`, keep the Flux `Kustomization` and
handle it like the Helm chart in step 9. Flux then needs the manifests as an OCI artifact, as in the
[kustomize-configuration-localization example](https://github.com/open-component-model/open-component-model/tree/main/bindings/go/kubernetes/controller/examples/kustomize-configuration-localization).
A `helmReleaseTemplate` has no OCM replacement: its `HelmRelease` stays, and step 9 points it at the chart in your
registry.

{{< tabs "fluxdeployer" >}}
{{< tab "Before" >}}

```yaml
spec:
  sourceRef:
    kind: Resource
    name: podinfo-manifests
  kustomizationTemplate:
    path: ./
    prune: true
    targetNamespace: my-team
```

{{< /tab >}}
{{< tab "After: namespaced-deployer.yaml" >}}

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: NamespacedDeployer
metadata:
  name: podinfo
  namespace: my-team
spec:
  resourceRef:
    name: podinfo-manifests
  serviceAccountName: podinfo-deployer
```

{{< /tab >}}
{{< /tabs >}}

**Configuration and Localization.** There is no replacement kind. Configure and localize plain manifests before you
publish the component version. For Helm charts, set values in `HelmRelease.spec.values` and take image references
from the status of an image `Resource`, as in the
[helm-configuration-localization example](https://github.com/open-component-model/open-component-model/tree/main/bindings/go/kubernetes/controller/examples/helm-configuration-localization).

### 2. Back up the legacy objects

```bash
kubectl get componentversions.delivery.ocm.software,resources.delivery.ocm.software,fluxdeployers.delivery.ocm.software,configurations.delivery.ocm.software,localizations.delivery.ocm.software \
  --all-namespaces -o yaml > legacy-ocm-objects.yaml
```

List the Flux objects that the `FluxDeployers` created:

```bash
kubectl get kustomizations.kustomize.toolkit.fluxcd.io,helmreleases.helm.toolkit.fluxcd.io,ocirepositories.source.toolkit.fluxcd.io \
  --all-namespaces -o json \
  | jq -r '.items[] | select(any(.metadata.ownerReferences[]?; .kind == "FluxDeployer")) | "\(.kind) \(.metadata.namespace)/\(.metadata.name)"'
```

You should see an `OCIRepository` and a `Kustomization` or `HelmRelease` per `FluxDeployer`, with its name.

### 3. Detach the Flux objects from the FluxDeployers

Each Flux object has an owner reference to its `FluxDeployer`. If you delete the `FluxDeployer` now, the garbage
collector deletes the `Kustomization` or `HelmRelease`, and Flux prunes the workload or uninstalls the release.
Remove the owner reference from every object in the list:

```bash
kubectl patch kustomization.kustomize.toolkit.fluxcd.io podinfo -n my-team --type json \
  -p '[{"op":"remove","path":"/metadata/ownerReferences"}]'
kubectl patch ocirepository.source.toolkit.fluxcd.io podinfo -n my-team --type json \
  -p '[{"op":"remove","path":"/metadata/ownerReferences"}]'
```

Patch `helmrelease.helm.toolkit.fluxcd.io/podinfo-chart` and `ocirepository.source.toolkit.fluxcd.io/podinfo-chart`
the same way. Run the list command from step 2 again. It should print nothing. Then delete the `FluxDeployers`:

```bash
kubectl delete fluxdeployers.delivery.ocm.software --all --all-namespaces
```

Suspend every `Kustomization` and `HelmRelease`. Do this after deleting the `FluxDeployers`, because the legacy
controller rewrites their `spec` on every reconciliation.

```bash
kubectl patch kustomization.kustomize.toolkit.fluxcd.io podinfo -n my-team --type merge -p '{"spec":{"suspend":true}}'
kubectl patch helmrelease.helm.toolkit.fluxcd.io podinfo-chart -n my-team --type merge -p '{"spec":{"suspend":true}}'
```

Flux does not prune the objects of a suspended `Kustomization` when you delete it, and it does not uninstall the
release of a suspended `HelmRelease`.

### 4. Delete the remaining legacy objects

Delete them while the legacy controller still runs. It removes the finalizer of each `Snapshot` after deleting its
data from the in-cluster registry.

```bash
kubectl delete localizations.delivery.ocm.software,configurations.delivery.ocm.software,resources.delivery.ocm.software,componentversions.delivery.ocm.software \
  --all --all-namespaces
kubectl get snapshots.delivery.ocm.software,componentdescriptors.delivery.ocm.software --all-namespaces
```

You should see: `No resources found`. The garbage collector deletes `Snapshots` and `ComponentDescriptors` through
their owner references, which can take a moment.

### 5. Remove the legacy controller and its registry

{{< tabs "uninstall" >}}
{{< tab "Installed with Helm" >}}

```bash
helm uninstall ocm-controller -n ocm-system
```

This removes the controller, the registry and their RBAC. Helm keeps the CRDs.

{{< /tab >}}
{{< tab "Installed with ocm controller install" >}}

```bash
kubectl delete -n ocm-system deployment/ocm-controller deployment/registry service/registry \
  serviceaccount/ocm-controller role/ocm-leader-election-role rolebinding/ocm-leader-election-rolebinding \
  secret/ocm-registry-tls-certs --ignore-not-found
kubectl delete clusterrole/ocm-controller-manager-role clusterrolebinding/ocm-controller-rolebinding --ignore-not-found
kubectl delete certificate.cert-manager.io/ocm-registry-certificate -n ocm-system --ignore-not-found
kubectl delete clusterissuer.cert-manager.io/ocm-issuer --ignore-not-found
```

`ocm controller install` also installed cert-manager. Keep it if other workloads use it.

{{< /tab >}}
{{< /tabs >}}

### 6. Delete the legacy CRDs

```bash
kubectl delete crd componentdescriptors.delivery.ocm.software componentversions.delivery.ocm.software \
  configurations.delivery.ocm.software fluxdeployers.delivery.ocm.software localizations.delivery.ocm.software \
  resources.delivery.ocm.software snapshots.delivery.ocm.software
kubectl get crd | grep delivery.ocm.software
```

The second command should print nothing.

### 7. Install the new controllers

```bash
helm install ocm-k8s-toolkit "oci://ghcr.io/open-component-model/kubernetes/controller/chart" \
  --namespace ocm-k8s-toolkit-system \
  --create-namespace
```

For the chart options, see
[Set up Controller Environments]({{< docref "docs/getting-started/setup-controller-environment.md" >}}).

### 8. Apply the new objects

Create the credential Secrets, then apply the files from step 1 except `namespaced-deployer.yaml`. Leave out
`replication.yaml` if no `ComponentVersion` had a `destination`.

```bash
kubectl apply -f component.yaml -f resource.yaml -f replication.yaml
kubectl wait component.delivery.ocm.software/podinfo -n my-team --for=condition=Ready --timeout=5m
kubectl wait resource.delivery.ocm.software --all -n my-team --for=condition=Ready --timeout=5m
```

### 9. Hand over the workloads

{{< tabs "handover" >}}
{{< tab "Plain manifests" >}}

Create the service account and grant it the kinds in your manifests, see
[Configure Custom RBAC for Deployers]({{< docref "docs/guides/deploy/configure-custom-rbac.md" >}}):

```bash
kubectl create serviceaccount podinfo-deployer -n my-team
kubectl create role podinfo-deployer -n my-team \
  --verb=get,list,watch,create,update,patch,delete --resource=deployments.apps,services
kubectl create rolebinding podinfo-deployer -n my-team --role=podinfo-deployer --serviceaccount=my-team:podinfo-deployer
```

Apply the `NamespacedDeployer` and wait until it is ready:

```bash
kubectl apply -f namespaced-deployer.yaml
kubectl wait namespaceddeployer.delivery.ocm.software/podinfo -n my-team --for=condition=Ready --timeout=5m
```

The objects that Flux applied carry no ApplySet label, so the `NamespacedDeployer` takes them over with server-side
apply. They keep their UIDs. Check one of them:

```bash
kubectl get deployment podinfo -n my-team -o jsonpath='{.metadata.ownerReferences[*].kind}'
```

You should see: `NamespacedDeployer`. Then delete the suspended Flux objects:

```bash
kubectl delete kustomization.kustomize.toolkit.fluxcd.io podinfo -n my-team
kubectl delete ocirepository.source.toolkit.fluxcd.io podinfo -n my-team
```

{{< /tab >}}
{{< tab "Helm chart" >}}

Read the chart location from the `Resource`:

```bash
kubectl get resource.delivery.ocm.software podinfo-chart -n my-team -o jsonpath='{.status.additional.oci}'
```

Save the following as `ocirepository.yaml`, with `registry`, `repository` and `tag` from the output. It replaces the
`OCIRepository` that pointed to the legacy registry.

```yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: podinfo-chart
  namespace: my-team
spec:
  interval: 10m
  url: oci://ghcr.io/my-org/components/charts/podinfo
  ref:
    tag: 6.9.1
  layerSelector:
    mediaType: application/vnd.cncf.helm.chart.content.v1.tar+gzip
    operation: copy
```

```bash
kubectl replace -f ocirepository.yaml
kubectl patch helmrelease.helm.toolkit.fluxcd.io podinfo-chart -n my-team --type merge -p '{"spec":{"suspend":false}}'
```

The `HelmRelease` keeps the values of your `helmReleaseTemplate`, its release name, its namespace and its chart, so
Flux upgrades the existing release instead of reinstalling it. To let kro create these Flux objects from the
`Resource` status, see
[Deploy an Application from a Helm Chart with OCM and kro]({{< docref "docs/guides/deploy/deploy-helm-chart-with-kro-and-gitops.md" >}}).

{{< /tab >}}
{{< /tabs >}}

### 10. Verify the migration

```bash
kubectl wait repositories.delivery.ocm.software,components.delivery.ocm.software,resources.delivery.ocm.software,namespaceddeployers.delivery.ocm.software \
  --all -n my-team --for=condition=Ready --timeout=5m
kubectl wait helmreleases.helm.toolkit.fluxcd.io --all -n my-team --for=condition=Ready --timeout=5m
```

You should see `condition met` for every object. Add `replications.delivery.ocm.software` if you created a
`Replication`.

## Troubleshooting

### Symptom: `CustomResourceDefinition "resources.delivery.ocm.software" ... exists and cannot be imported into the current release`

**Cause:** You ran `helm install` for the new controllers while the legacy CRDs exist.

**Fix:** Complete steps 2 to 6, then install again.

### Symptom: `kubectl delete crd` does not return, and the CRD reports `CustomResource deletion is in progress`

**Cause:** A `Snapshot` still has the finalizer `finalizers.snapshot.ocm.software`, and the legacy controller that
removes it is gone.

**Fix:** Remove the finalizer. Its data went away with the legacy registry.

```bash
kubectl patch snapshots.delivery.ocm.software <name> -n <namespace> --type json \
  -p '[{"op":"remove","path":"/metadata/finalizers"}]'
```

### Symptom: editing a `Resource` fails with `spec.componentRef: Required value`

**Cause:** It is a legacy `Resource` that survived the CRD swap. The new schema does not know its fields.

**Fix:** Delete it and create the new `Resource` from step 1.

## Related Documentation

- [Key Differences Between OCM v1 and v2]({{< docref "docs/changelog/differences-v1-v2.md" >}})
- [Blog: What changed between legacy and new OCM Controllers]({{< docref "blog/2026-03-16-ocm-controllers-differences.md" >}})
- [Concept: Kubernetes Controllers]({{< docref "docs/concepts/ocm-controllers.md" >}})
- [Concept: Kubernetes Deployer]({{< docref "docs/concepts/kubernetes-deployer.md" >}})
- [Guides: Configure Credentials for OCM Controllers]({{< docref "docs/guides/deploy/configure-controller-credentials.md" >}})
- [Guides: Replicate Component Versions with the Controller]({{< docref "docs/guides/transfer/replicate-component-versions.md" >}})
- [Migrate a Deployer to a NamespacedDeployer]({{< docref "docs/changelog/migrate-to-namespaced-deployer.md" >}})
