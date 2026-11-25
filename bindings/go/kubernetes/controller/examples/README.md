# Examples

Each directory contains the component constructor, Kubernetes configuration, and a standalone `Taskfile.yml`
needed to prepare that example. Most examples deploy through kro and Crossplane; the raw-manifest and replication
examples use their own manifests. Follow the shared steps below and the example's section to run it without Go
test code. The e2e suite uses these same recipes and configuration files.

## Prerequisites

The supported local route uses a dedicated `ocm-e2e` Kind cluster. Install Docker, Go (matching
`bindings/go/go.mod`), Task, Flux CLI, Helm, jq, kind, and kubectl. Kustomize localization also needs
[ORAS](https://oras.land/docs/installation). Image downloads require network access.

From the **repository root**:

```bash
task bindings/go/cli:build
export OCM_CLI="$PWD/bindings/go/cli/tmp/bin/ocm"
task bindings/go/kubernetes/controller:test/e2e/setup/local
export CONTROLLER_IMG=ghcr.io/open-component-model/kubernetes/controller:local-e2e
task bindings/go/kubernetes/controller:docker-build LOAD=true PUSH=false
kind load docker-image "$CONTROLLER_IMG" --name ocm-e2e
task bindings/go/kubernetes/controller:helm/install KUBE_CONTEXT=kind-ocm-e2e \
  VALUES=chart/test-values.yaml IMAGE_REPOSITORY="${CONTROLLER_IMG%:*}" \
  IMAGE_TAG="${CONTROLLER_IMG##*:}" IMAGE_PULL_POLICY=Never
kubectl --context=kind-ocm-e2e -n ocm-k8s-toolkit-system rollout restart deployment \
  -l app.kubernetes.io/instance=ocm-k8s-toolkit,control-plane=controller-manager
kubectl --context=kind-ocm-e2e -n ocm-k8s-toolkit-system rollout status deployment \
  -l app.kubernetes.io/instance=ocm-k8s-toolkit,control-plane=controller-manager --timeout=120s
export IMAGE_REGISTRY=http://ocm-e2e-image-registry:5000
```

Setup creates the registries and Docker aliases, kro and its RBAC, Flux, ArgoCD, and Crossplane with its
composition functions and RBAC. ArgoCD receives its default AppProject, plain-HTTP registry credential templates,
and the Flux OCI layer-media-type allowlist. Setup may request sudo to add the registry hostname to `/etc/hosts`.
Setup only provisions infrastructure. The separate build/load/install commands select the current controller
source without changing release pins; rollout restart replaces pods even when `local-e2e` is reused.
Do not recreate an existing cluster without checking who uses it.

`OCM_CLI` must be a command on PATH or an absolute executable path. An installed current OCM v2 CLI can replace
the local build. See the [controller installation notes](../README.md#installation) for other environments.

Each example ships `configuration.yaml` with its own `<example>-plain-http` ConfigMap and `transfer.ocmconfig`
with matching CLI HTTP settings. Both allow plain HTTP to the local registry while keeping ghcr.io on HTTPS.
For a TLS registry, change the repository URLs in the manifests and `IMAGE_REGISTRY`, adjust matching credential
identities, and remove the HTTP configuration entry from both files. Changing `IMAGE_REGISTRY` alone does not
rewrite manifests or credentials.

## Example overview

| Example | Deployer(s) | What it demonstrates |
|---|---|---|
| [`helm-simple`](#helm-simple) | FluxCD + ArgoCD + Crossplane | Minimal Helm chart deploy from an OCI artifact |
| [`helm-nested`](#helm-nested) | FluxCD + ArgoCD + Crossplane | Helm chart buried in a nested OCM component reference |
| [`helm-nested-signed`](#helm-nested-signed) | FluxCD + ArgoCD + Crossplane | Nested component with signature verification |
| [`helm-signing`](#helm-signing) | FluxCD + ArgoCD + Crossplane | Deploying a signed Helm chart with key verification |
| [`helm-simple-nested-status`](#helm-simple-nested-status) | FluxCD + ArgoCD + Crossplane | Exposing nested resource status fields through `additionalStatusFields` |
| [`helm-configuration-localization`](#helm-configuration-localization) | FluxCD + ArgoCD + Crossplane | Image localization + Helm value injection |
| [`kustomize-simple`](#kustomize-simple) | FluxCD + ArgoCD + Crossplane | Kustomize overlay from a Git-sourced OCM resource |
| [`kustomize-configuration-localization`](#kustomize-configuration-localization) | FluxCD + ArgoCD + Crossplane | Image localization via Kustomize JSON patches |
| [`k8s-manifest-simple`](#k8s-manifest-simple) | `NamespacedDeployer` | Applying a plain Kubernetes manifest from an OCM resource |
| [`k8s-manifest-cross-namespace`](#k8s-manifest-cross-namespace) | `NamespacedDeployer` | Applying into a second namespace that granted the deployer's service account |
| [`applyset-pruning`](#applyset-pruning) | `NamespacedDeployer` | Pruning orphaned resources with ApplySet |
| [`replication-simple`](#replication-simple) | Replication controller | Copying a component into a protected registry |

Deployment examples use the `NamespacedDeployer`, which applies with the
permissions of a service account defined in the example's bootstrap. The RGD examples bind that service account
to a `ClusterRole` for `ResourceGraphDefinitions`, as RGDs are cluster-scoped. The cluster-scoped `Deployer` is
discouraged because it applies with the controller's own permissions.

The kro `rgd.yaml` bundles **both the FluxCD and ArgoCD deployer blocks** in the same file; kro instantiates
both. Crossplane is a separate path, in its own `crossplane-composition.yaml`. On a cluster where only one
GitOps deployer is installed, remove the block for the absent deployer from `rgd.yaml`.

Crossplane Compositions run under the `crossplane` service account, which by default cannot create the
resources they produce (OCM `Resource`s, Flux sources/`HelmRelease`s/`Kustomization`s, ArgoCD `Application`s),
so the XR never becomes Ready. Grant the Crossplane service account these permissions once per cluster with an
aggregated `ClusterRole` that Crossplane's RBAC manager folds into that service account (no binding needed):

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: crossplane-ocm-resources
  labels:
    rbac.crossplane.io/aggregate-to-crossplane: "true"
rules:
  - apiGroups: ["delivery.ocm.software"]
    resources: ["resources", "resources/status"]
    verbs: ["create", "delete", "get", "list", "patch", "update", "watch"]
  - apiGroups: ["source.toolkit.fluxcd.io"]
    resources: ["ocirepositories", "ocirepositories/status", "gitrepositories", "gitrepositories/status"]
    verbs: ["create", "delete", "get", "list", "patch", "update", "watch"]
  - apiGroups: ["kustomize.toolkit.fluxcd.io"]
    resources: ["kustomizations", "kustomizations/status"]
    verbs: ["create", "delete", "get", "list", "patch", "update", "watch"]
  - apiGroups: ["helm.toolkit.fluxcd.io"]
    resources: ["helmreleases", "helmreleases/status"]
    verbs: ["create", "delete", "get", "list", "patch", "update", "watch"]
  - apiGroups: ["argoproj.io"]
    resources: ["applications", "applications/status"]
    verbs: ["create", "delete", "get", "list", "patch", "update", "watch"]
```

On a shared cluster, scope this down to the groups and namespaces you actually use. The local e2e setup grants
the same permissions from `test/e2e/hacks/rbac.yaml`.

## Preparing a component

From the example directory, `task prepare` builds, signs where applicable, and transfers the component into
`IMAGE_REGISTRY`. Its temporary CTF is removed on exit. The example sections set `EXAMPLE` and show which
deployment flow follows preparation.

To execute the CLI steps directly instead, use this sequence from the same directory:

```bash
work=$(mktemp -d)
trap 'rm -r -- "$work"' EXIT
"$OCM_CLI" add cv --repository "$work/ctf" --constructor ./component-constructor.yaml
# For signed examples, run their explicit sign commands below before transferring.
"$OCM_CLI" transfer cv "ctf::$work/ctf//ocm.software/ocm-k8s-toolkit/examples/$EXAMPLE" \
  "$IMAGE_REGISTRY" --config ./transfer.ocmconfig
```

Run either the Task recipe or the direct sequence, not both. For Kustomize localization, use its complete
[staged direct sequence](kustomize-configuration-localization/README.md#how-the-kustomize-artifact-is-built)
instead of the generic construction and transfer commands above.
Nested examples declare `recursive: -1` in a `transfer.config.ocm.software/v1alpha1` entry, replacing the need
to pass `--recursive`. Both localization examples declare these uploaders, in order:

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
- type: localblob.uploader.transfer.config.ocm.software/v1alpha1
```

The OCI uploader creates native OCI artifacts, and the local-blob uploader copies the remaining resources.
These native configuration entries replace the deprecated `--copy-resources` flag. No options are inferred
from the example name.

## Deploying kro and Crossplane examples

Use this flow for all six Helm examples and both Kustomize examples, after setting `EXAMPLE` and preparing
the component from its directory. All commands explicitly target the dedicated cluster.

```bash
kubectl --context=kind-ocm-e2e -n default apply -f configuration.yaml
kubectl --context=kind-ocm-e2e -n default apply -f bootstrap.yaml
kubectl --context=kind-ocm-e2e wait "rgd/$EXAMPLE" --for=create --timeout=5m
kubectl --context=kind-ocm-e2e wait "rgd/$EXAMPLE" --for=condition=Ready=true --timeout=5m
kubectl --context=kind-ocm-e2e -n default apply -f kro-instance.yaml
kubectl --context=kind-ocm-e2e -n default wait -f kro-instance.yaml --for=condition=Ready=true --timeout=5m

XRD="$(printf '%s' "$EXAMPLE" | tr -d '-')crossplanes.examples.ocm.software"
kubectl --context=kind-ocm-e2e wait "compositeresourcedefinitions.apiextensions.crossplane.io/$XRD" \
  --for=create --timeout=5m
kubectl --context=kind-ocm-e2e wait "compositeresourcedefinitions.apiextensions.crossplane.io/$XRD" \
  --for=condition=Established=true --timeout=5m
kubectl --context=kind-ocm-e2e -n default apply -f crossplane-instance.yaml
kubectl --context=kind-ocm-e2e -n default wait -f crossplane-instance.yaml --for=condition=Ready=true --timeout=5m

for namespace in default default-argocd; do
  for name in "$EXAMPLE-podinfo" "$EXAMPLE-crossplane-podinfo"; do
    kubectl --context=kind-ocm-e2e -n "$namespace" wait "deployment.apps/$name" --for=create --timeout=5m
    kubectl --context=kind-ocm-e2e -n "$namespace" wait "deployment.apps/$name" --for=condition=Available --timeout=5m
  done
done
for name in "$EXAMPLE" "$EXAMPLE-crossplane"; do
  kubectl --context=kind-ocm-e2e -n argocd wait "applications.argoproj.io/$name" \
    --for=jsonpath='{.status.sync.status}=Synced' --timeout=5m
done
```

Bootstrap deploys the RGD and XRD/Composition **from the OCM resources**; do not separately apply `rgd.yaml`
or `crossplane-composition.yaml`. Never use `kubectl apply -f .`: the directory also contains constructors,
OCM configuration, and Taskfiles that are not Kubernetes manifests.

## Deploying raw manifests

For either `k8s-manifest-*` example, prepare the component and run:

```bash
kubectl --context=kind-ocm-e2e -n default apply -f configuration.yaml
kubectl --context=kind-ocm-e2e -n default apply -f bootstrap.yaml
kubectl --context=kind-ocm-e2e -n default wait "deployment.apps/$EXAMPLE-podinfo" --for=create --timeout=5m
kubectl --context=kind-ocm-e2e -n default wait "deployment.apps/$EXAMPLE-podinfo" --for=condition=Available --timeout=5m
```

## Removing an example

Keep configuration and deployer permissions present until finalization finishes. For kro/Crossplane examples,
remove instances, then both deployers, then bootstrap and configuration:

```bash
kubectl --context=kind-ocm-e2e -n default delete -f kro-instance.yaml -f crossplane-instance.yaml --wait=true
kubectl --context=kind-ocm-e2e -n default delete namespaceddeployers.delivery.ocm.software \
  "$EXAMPLE-deployer" "$EXAMPLE-crossplane-deployer" --wait=true
kubectl --context=kind-ocm-e2e -n default delete -f bootstrap.yaml --ignore-not-found --wait=true
kubectl --context=kind-ocm-e2e -n default delete -f configuration.yaml --wait=true
```

For raw manifests, skip the instance deletion and delete only `"$EXAMPLE-deployer"` before the last two
commands. Pruning and replication have their teardown commands in their own sections.

---

## helm-simple

**Deployers:** FluxCD (`OCIRepository` + `HelmRelease`), ArgoCD (`Application`), and Crossplane (`crossplane-composition.yaml`)

The baseline example. A single Helm chart resource is resolved from an OCM component and deployed via both
FluxCD and ArgoCD. This is the best starting point for understanding how the toolkit's resource pipeline works.

The `Resource` exposes OCI coordinates (`registry`, `repository`, `digest`, `tag`) in `status.additional.*`.
FluxCD pins to the digest for secure pulls; ArgoCD references the tag (digest pinning is not supported for
ArgoCD Helm OCI sources).

```bash
EXAMPLE=helm-simple
task prepare
```

Then follow [Deploying kro and Crossplane examples](#deploying-kro-and-crossplane-examples).

---

## helm-nested

**Deployers:** FluxCD + ArgoCD + Crossplane

Demonstrates how to reference a Helm chart that lives inside a _nested_ OCM component reference (i.e., an OCM
component version that itself references another component). The `Resource` spec uses `referencePath` to
navigate the nesting:

```yaml
resource:
  byReference:
    resource:
      name: helm-resource
    referencePath:
      - name: nested-chart
```

```bash
EXAMPLE=helm-nested
task prepare
```

Its `transfer.ocmconfig` declares recursive transfer. Then follow the
[kro/Crossplane deployment flow](#deploying-kro-and-crossplane-examples).

---

## helm-nested-signed

**Deployers:** FluxCD + ArgoCD + Crossplane

Like `helm-nested`, but the component version is signed. The controller verifies it using the public-key-only
Secret in `configuration.yaml` before exposing the OCI artifact coordinates. `signing.ocmconfig` contains a
known test private key used **only locally** by the signing command; never apply it to Kubernetes.

```bash
EXAMPLE=helm-nested-signed
task prepare
```

For the direct CLI sequence, insert this between construction and transfer:

```bash
"$OCM_CLI" sign cv "ctf::$work/ctf//ocm.software/ocm-k8s-toolkit/examples/helm-nested-signed:1.0.0" \
  --signature ocm.software.helm.nested.signed.rsa --config ./signing.ocmconfig
```

Then follow the [kro/Crossplane deployment flow](#deploying-kro-and-crossplane-examples).

---

## helm-signing

**Deployers:** FluxCD + ArgoCD + Crossplane

Demonstrates RSA and GPG signature verification for a top-level OCM resource. `signing.ocmconfig` holds local
test private keys; `configuration.yaml` ships the verifier-only Secret with public keys and no passphrase.
These are intentionally published test keys, not production credentials.

```bash
EXAMPLE=helm-signing
task prepare
```

For the direct CLI sequence, insert both commands between construction and transfer. The explicit signature
names are required; the CLI's `default` signature does not select these credentials.

```bash
"$OCM_CLI" sign cv "ctf::$work/ctf//ocm.software/ocm-k8s-toolkit/examples/helm-signing:1.0.0" \
  --signature ocm.software.helm.signing.rsa --config ./signing.ocmconfig
"$OCM_CLI" sign cv "ctf::$work/ctf//ocm.software/ocm-k8s-toolkit/examples/helm-signing:1.0.0" \
  --signature ocm.software.helm.signing.gpg --config ./signing.ocmconfig
```

Then follow the [kro/Crossplane deployment flow](#deploying-kro-and-crossplane-examples).

---

## helm-simple-nested-status

**Deployers:** FluxCD + ArgoCD + Crossplane

Shows how to surface additional fields from an OCM resource's access information using `additionalStatusFields`.
Useful when the RGD needs to pass registry coordinates (registry, repository, digest, tag) to downstream
deployer resources via kro's `${...}` CEL expressions.

```bash
EXAMPLE=helm-simple-nested-status
task prepare
```

Then follow the [kro/Crossplane deployment flow](#deploying-kro-and-crossplane-examples).

---

## helm-configuration-localization

**Deployers:** FluxCD + ArgoCD + Crossplane

The most comprehensive Helm example. Demonstrates two capabilities together:

- **Localization** — the chart's `image.repository` and `image.tag` are replaced with the transferred OCI image
  location, so the workload pulls from the local registry rather than the original upstream.
- **Configuration** — a user-defined `ui.message` value is injected into the running pod via Helm values.

FluxCD injects values through `HelmRelease.spec.values`; ArgoCD through
`Application.spec.source.helm.valuesObject`. The structured `valuesObject` field avoids escaping issues that
arise with the string-based `values` field.

```bash
EXAMPLE=helm-configuration-localization
task prepare
```

Then follow the [kro/Crossplane deployment flow](#deploying-kro-and-crossplane-examples).

---

## kustomize-simple

**Deployers:** FluxCD (`GitRepository` + `Kustomization`), ArgoCD (`Application` with `source.path`), and Crossplane (`crossplane-composition.yaml`)

Deploys a Kustomize overlay sourced from a Git repository referenced through an OCM resource. The commit SHA is
taken from `resource.status.resource.access.commit` and pinned in both the FluxCD `GitRepository` ref and the
ArgoCD `Application.spec.source.targetRevision`.

```bash
EXAMPLE=kustomize-simple
task prepare
```

Then follow the [kro/Crossplane deployment flow](#deploying-kro-and-crossplane-examples).

---

## kustomize-configuration-localization

**Deployers:** FluxCD + ArgoCD + Crossplane

Ships its Kustomize manifests as a Flux-consumable OCI artifact, unlike the Git-sourced `kustomize-simple`.
It adds image localization and configuration via JSON Patch operations:

- **Localization** — replaces `spec.template.spec.containers[0].image` with the transferred OCI image reference.
- **Configuration** — injects `PODINFO_UI_MESSAGE` as an environment variable.

> **Note on kro + ArgoCD kustomize patches:** kro cannot parse multi-line YAML block scalars that contain
> `${…}` CEL expression references inside ArgoCD `kustomize.patches`. The workaround is to express those
> patches as inline JSON arrays (e.g. `'[{"op":"replace",…}]'`). Static patches (no CEL expressions) can
> continue to use normal YAML block scalars. See the `rgd.yaml` in this example for the pattern.

```bash
EXAMPLE=kustomize-configuration-localization
task prepare
```

ORAS is required. The recipe packs the layout in a temporary staging directory and uses the shipped OCI
uploaders. See its [packaging instructions](kustomize-configuration-localization/README.md), then follow the
[kro/Crossplane deployment flow](#deploying-kro-and-crossplane-examples).

---

## k8s-manifest-simple

**Deployer:** `NamespacedDeployer`

Applies a raw Kubernetes manifest stored as an OCM resource, without using kro or a GitOps deployer. Useful as
a reference for the simplest possible OCM → cluster workflow. The `NamespacedDeployer` applies with the
permissions of the `k8s-manifest-simple-deployer` service account.

```bash
EXAMPLE=k8s-manifest-simple
task prepare
```

Then follow [Deploying raw manifests](#deploying-raw-manifests).

---

## k8s-manifest-cross-namespace

**Deployer:** `NamespacedDeployer`

Deploys one OCM resource into two namespaces. The `Deployment` carries no namespace and lands next to the
`NamespacedDeployer`. The dashboard `ConfigMap` names the `k8s-manifest-cross-namespace-monitoring` namespace.

The service account's RBAC is the only thing that allows this. The bootstrap grants it `deployments` in its own
namespace, and a second `Role` and `RoleBinding` inside the monitoring namespace grant it `configmaps` there.
Remove that second pair and the `NamespacedDeployer` reports `ApplyFailed` with a `forbidden` error.

Objects in another namespace cannot carry an owner reference to the `NamespacedDeployer`, so garbage collection
does not remove them. Delete the `NamespacedDeployer` before its service account and RBAC. Without the RBAC the
deletion waits with a `DeletionFailed` condition, and without the service account the `ConfigMap` is orphaned and
listed in a `DeletionFailed` warning event.

```bash
EXAMPLE=k8s-manifest-cross-namespace
task prepare
```

Follow [Deploying raw manifests](#deploying-raw-manifests), then verify the dashboard in the namespace and RBAC
shipped by this bootstrap:

```bash
kubectl --context=kind-ocm-e2e -n k8s-manifest-cross-namespace-monitoring \
  get configmap/k8s-manifest-cross-namespace-dashboard
```

---

## applyset-pruning

**Deployer:** `NamespacedDeployer` (ApplySet-based pruning)

Demonstrates how orphaned resources are pruned when an OCM component version is updated. Uses Kubernetes
[ApplySet](https://kubernetes.io/docs/reference/labels-annotations-taints/) for pruning instead of a
GitOps deployer. This example has its own dedicated e2e test file (`test/e2e/e2e_applyset_test.go`) and is
excluded from the generic examples test loop.

From `applyset-pruning/`, publish v1 and deploy both workloads:

```bash
EXAMPLE=applyset-pruning
task prepare
kubectl --context=kind-ocm-e2e -n default apply -f configuration.yaml
kubectl --context=kind-ocm-e2e -n default apply -f bootstrap.yaml
kubectl --context=kind-ocm-e2e -n default apply -f bootstrap-deployer.yaml
for name in applyset-pruning-podinfo applyset-pruning-podinfo-2; do
  kubectl --context=kind-ocm-e2e -n default wait "deployment.apps/$name" --for=create --timeout=5m
  kubectl --context=kind-ocm-e2e -n default wait "deployment.apps/$name" --for=condition=Available --timeout=5m
done
```

Publish v2 with the same recipe and select it on the Component:

```bash
task prepare CONSTRUCTOR=component-constructor-2.yaml
kubectl --context=kind-ocm-e2e -n default patch component.delivery.ocm.software/applyset-pruning-component \
  --type=merge -p '{"spec":{"semver":"2.0.0"}}'
kubectl --context=kind-ocm-e2e -n default wait component.delivery.ocm.software/applyset-pruning-component \
  --for=jsonpath='{.status.component.version}=2.0.0' --timeout=5m
kubectl --context=kind-ocm-e2e -n default wait deployment.apps/applyset-pruning-podinfo-2 \
  --for=delete --timeout=5m
kubectl --context=kind-ocm-e2e -n default wait deployment.apps/applyset-pruning-podinfo \
  --for=condition=Available --timeout=5m
```

For direct CLI publication of v2 instead of the second Task invocation, use a fresh CTF:

```bash
work=$(mktemp -d)
trap 'rm -r -- "$work"' EXIT
"$OCM_CLI" add cv --repository "$work/ctf" --constructor ./component-constructor-2.yaml
"$OCM_CLI" transfer cv "ctf::$work/ctf//ocm.software/ocm-k8s-toolkit/examples/applyset-pruning:2.0.0" \
  "$IMAGE_REGISTRY" --config ./transfer.ocmconfig
```

Only `applyset-pruning-podinfo-2` disappears; the first Deployment remains Available. Remove the example in
dependency order:

```bash
kubectl --context=kind-ocm-e2e -n default delete namespaceddeployer.delivery.ocm.software/applyset-pruning-deployer \
  --wait=true
kubectl --context=kind-ocm-e2e -n default delete -f bootstrap-deployer.yaml -f bootstrap.yaml \
  --ignore-not-found --wait=true
kubectl --context=kind-ocm-e2e -n default delete -f configuration.yaml --wait=true
```

---

## replication-simple

Replicates a component into a protected registry using test credentials `admin/admin`. The shared setup
creates the source registry's Docker alias `http://image-registry:5000`, the in-cluster target
`http://protected-registry1-internal.default.svc.cluster.local:5002`, and its host NodePort
`http://localhost:31002`. The source alias reaches the same registry as `IMAGE_REGISTRY`.

From `replication-simple/`:

```bash
EXAMPLE=replication-simple
task prepare
kubectl --context=kind-ocm-e2e -n default apply -f configuration.yaml
kubectl --context=kind-ocm-e2e -n default apply -f manifests.yaml
kubectl --context=kind-ocm-e2e -n default wait component.delivery.ocm.software/replication-simple-component \
  --for=condition=Ready=true --timeout=5m
kubectl --context=kind-ocm-e2e -n default wait repository.delivery.ocm.software/replication-simple-target \
  --for=condition=Ready=true --timeout=5m
kubectl --context=kind-ocm-e2e -n default wait replication.delivery.ocm.software/replication-simple \
  --for=condition=Ready=true --timeout=5m
kubectl --context=kind-ocm-e2e -n default wait replication.delivery.ocm.software/replication-simple \
  --for=jsonpath='{.status.lastTransferredVersion}=1.0.0' --timeout=5m
"$OCM_CLI" --config ./.ocmconfig --config ./transfer.ocmconfig get cv \
  'http://localhost:31002//ocm.software/ocm-k8s-toolkit/examples/replication-simple:1.0.0'
```

The final command prints the component name and version `1.0.0`. `manifests.yaml` ships the target credentials
Secret; `.ocmconfig` contains host-side registry credentials, not signing keys. No signing or extra Secret
creation is needed. For another target, update manifest URLs and both credential identities, not just the
host-side CLI reference.

Remove replication and its manifests before the HTTP configuration:

```bash
kubectl --context=kind-ocm-e2e -n default delete replication.delivery.ocm.software/replication-simple --wait=true
kubectl --context=kind-ocm-e2e -n default delete -f manifests.yaml --ignore-not-found --wait=true
kubectl --context=kind-ocm-e2e -n default delete -f configuration.yaml --wait=true
```

---

## Running all examples

Run these harness commands from `bindings/go/kubernetes/controller/`. The normal suite includes both dedicated
examples as well as credential and migration scenarios, with four parallel processes.

```bash
# Run all examples, including the dedicated pruning and replication tests
task test/e2e

# Teardown the cluster, rebuild from scratch, and run all examples
task test/e2e/fresh

# Run a single example by name
task test/e2e -- --focus=helm-configuration-localization
```

See [Taskfile.yml](../Taskfile.yml) for the full list of e2e tasks and options.
