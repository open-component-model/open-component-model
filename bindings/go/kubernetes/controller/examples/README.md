# Examples

This directory contains end-to-end examples demonstrating how to use the OCM Kubernetes Controller Toolkit.
Each example is self-contained: it includes a component definition (`component-constructor.yaml`), a bootstrap
manifest (`bootstrap.yaml`), and — where deployment is needed — a kro `ResourceGraphDefinition` (`rgd.yaml`)
and an instance manifest (`instance.yaml`).

## Prerequisites

A local Kind cluster with kro, FluxCD, ArgoCD, and Crossplane installed. Use the Taskfile to set it up:

```bash
task test/e2e/setup/local
```

See the [installation notes in the root README](../README.md#installation) for details.

## Example overview

### kro-based examples

These examples use a kro `ResourceGraphDefinition` (`rgd.yaml`) to compose OCM resources into deployer-specific objects. Both FluxCD and ArgoCD deployer blocks live in the same `rgd.yaml`; remove the block for whichever is absent from your cluster.

| Example | Deployer(s) | What it demonstrates |
|---|---|---|
| [`helm-simple`](#helm-simple) | FluxCD + ArgoCD | Minimal Helm chart deploy from an OCI artifact |
| [`helm-nested`](#helm-nested) | FluxCD + ArgoCD | Helm chart buried in a nested OCM component reference |
| [`helm-nested-signed`](#helm-nested-signed) | FluxCD + ArgoCD | Nested component with signature verification |
| [`helm-signing`](#helm-signing) | FluxCD + ArgoCD | Deploying a signed Helm chart with key verification |
| [`helm-simple-nested-status`](#helm-simple-nested-status) | FluxCD + ArgoCD | Exposing nested resource status fields through `additionalStatusFields` |
| [`helm-configuration-localization`](#helm-configuration-localization) | FluxCD + ArgoCD | Image localization + Helm value injection |
| [`kustomize-simple`](#kustomize-simple) | FluxCD + ArgoCD | Kustomize overlay from a Git-sourced OCM resource |
| [`kustomize-configuration-localization`](#kustomize-configuration-localization) | FluxCD + ArgoCD | Image localization via Kustomize JSON patches |
| [`k8s-manifest-simple`](#k8s-manifest-simple) | (raw kubectl) | Applying a plain Kubernetes manifest from an OCM resource |
| [`applyset-pruning`](#applyset-pruning) | (raw kubectl) | Pruning orphaned resources with ApplySet |

### Crossplane-based examples

These examples replace kro with Crossplane Compositions. The XRD and Composition are stored as a `crossplane-xrd` blob resource inside the OCM component and applied to the cluster through an OCM `Deployer` — not from disk. The `bootstrap.yaml` contains the full OCM stack: `Repository` → `Component` → `Resource` (fetches the blob) → `Deployer` (applies XRD+Composition). Once the XRD is `Established`, creating the XR instance (`instance.yaml`) triggers the Composition pipeline.

| Example | Deployer(s) | What it demonstrates |
|---|---|---|
| [`helm/fluxcd/crossplane/simple`](#crossplane-simple) | FluxCD | Minimal Helm chart via Crossplane Composition + FluxCD `HelmRelease` |
| [`helm/argocd/crossplane/simple`](#crossplane-simple) | ArgoCD | Same scenario with ArgoCD `Application` |
| [`helm/fluxcd/crossplane/nested`](#crossplane-nested) | FluxCD | `referencePath` navigation through a nested OCM component |
| [`helm/argocd/crossplane/nested`](#crossplane-nested) | ArgoCD | Same with ArgoCD |
| [`helm/fluxcd/crossplane/nested-signed`](#crossplane-nested-signed) | FluxCD | Nested component with signature verification |
| [`helm/argocd/crossplane/nested-signed`](#crossplane-nested-signed) | ArgoCD | Same with ArgoCD |
| [`helm/fluxcd/crossplane/signing`](#crossplane-signing) | FluxCD | Signed top-level component |
| [`helm/argocd/crossplane/signing`](#crossplane-signing) | ArgoCD | Same with ArgoCD |
| [`helm/fluxcd/crossplane/simple-nested-status`](#crossplane-simple-nested-status) | FluxCD | `additionalStatusFields` surfaced through Composition |
| [`helm/argocd/crossplane/simple-nested-status`](#crossplane-simple-nested-status) | ArgoCD | Same with ArgoCD |
| [`helm/fluxcd/crossplane/configuration-localization`](#crossplane-configuration-localization) | FluxCD | Image localization + Helm value injection via Composition |
| [`helm/argocd/crossplane/configuration-localization`](#crossplane-configuration-localization) | ArgoCD | Same with ArgoCD |
| [`helm/fluxcd/crossplane/simple-fnc-kro`](#crossplane-simple-fnc-kro) | FluxCD | Crossplane Composition using `function-kro` to compose a kro `ResourceGraph` |

---

## helm-simple

**Deployers:** FluxCD (`OCIRepository` + `HelmRelease`) and ArgoCD (`Application`)

The baseline example. A single Helm chart resource is resolved from an OCM component and deployed via both
FluxCD and ArgoCD. This is the best starting point for understanding how the toolkit's resource pipeline works.

The `Resource` exposes OCI coordinates (`registry`, `repository`, `digest`, `tag`) in `status.additional.*`.
FluxCD pins to the digest for secure pulls; ArgoCD references the tag (digest pinning is not supported for
ArgoCD Helm OCI sources).

```bash
# Run this example in isolation
task test/e2e -- -ginkgo.focus=helm-simple
```

---

## helm-nested

**Deployers:** FluxCD + ArgoCD

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

---

## helm-nested-signed

**Deployers:** FluxCD + ArgoCD

Like `helm-nested`, but the OCM component version is signed. The `Resource` controller verifies the signature
using the public key provided in `ocm.software.pub` before exposing the OCI artifact coordinates.

---

## helm-signing

**Deployers:** FluxCD + ArgoCD

Demonstrates signature verification for a top-level (non-nested) OCM resource. The private key is used during
`task test/e2e` to sign the component version during the test setup phase; the controller verifies it at runtime.

---

## helm-simple-nested-status

**Deployers:** FluxCD + ArgoCD

Shows how to surface additional fields from an OCM resource's access information using `additionalStatusFields`.
Useful when the RGD needs to pass registry coordinates (registry, repository, digest, tag) to downstream
deployer resources via kro's `${...}` CEL expressions.

---

## helm-configuration-localization

**Deployers:** FluxCD + ArgoCD

The most comprehensive Helm example. Demonstrates two capabilities together:

- **Localization** — the chart's `image.repository` and `image.tag` are replaced with the transferred OCI image
  location, so the workload pulls from the local registry rather than the original upstream.
- **Configuration** — a user-defined `ui.message` value is injected into the running pod via Helm values.

FluxCD injects values through `HelmRelease.spec.values`; ArgoCD through
`Application.spec.source.helm.valuesObject`. The structured `valuesObject` field avoids escaping issues that
arise with the string-based `values` field.

```bash
task test/e2e -- -ginkgo.focus=helm-configuration-localization
```

---

## kustomize-simple

**Deployers:** FluxCD (`GitRepository` + `Kustomization`) and ArgoCD (`Application` with `source.path`)

Deploys a Kustomize overlay sourced from a Git repository referenced through an OCM resource. The commit SHA is
taken from `resource.status.resource.access.commit` and pinned in both the FluxCD `GitRepository` ref and the
ArgoCD `Application.spec.source.targetRevision`.

---

## kustomize-configuration-localization

**Deployers:** FluxCD + ArgoCD

Like `kustomize-simple`, but adds image localization and configuration via JSON Patch operations:

- **Localization** — replaces `spec.template.spec.containers[0].image` with the transferred OCI image reference.
- **Configuration** — injects `PODINFO_UI_MESSAGE` as an environment variable.

> **Note on kro + ArgoCD kustomize patches:** kro cannot parse multi-line YAML block scalars that contain
> `${…}` CEL expression references inside ArgoCD `kustomize.patches`. The workaround is to express those
> patches as inline JSON arrays (e.g. `'[{"op":"replace",…}]'`). Static patches (no CEL expressions) can
> continue to use normal YAML block scalars. See the `rgd.yaml` in this example for the pattern.

---

## k8s-manifest-simple

**Deployer:** none (raw `kubectl apply`)

Applies a raw Kubernetes manifest stored as an OCM resource, without using kro or a GitOps deployer. Useful as
a reference for the simplest possible OCM → cluster workflow.

---

## applyset-pruning

**Deployer:** none (ApplySet-based pruning)

Demonstrates how orphaned resources are pruned when an OCM component version is updated. Uses Kubernetes
[ApplySet](https://kubernetes.io/docs/reference/labels-annotations-taints/) for pruning instead of a
GitOps deployer. This example has its own dedicated e2e test file (`test/e2e/e2e_applyset_test.go`) and is
excluded from the generic examples test loop.

---

## Crossplane examples

All Crossplane examples follow the same structure. The XRD and Composition are bundled as a `crossplane-xrd` blob resource in the OCM component. `bootstrap.yaml` contains the full OCM stack:

```
Repository → Component → Resource (crossplane-xrd) → Deployer
```

Applying `bootstrap.yaml` starts the OCM controller chain. The `Deployer` applies the XRD and Composition to the cluster. Once the XRD reaches `Established`, apply `instance.yaml` to create the XR. The Composition pipeline then creates the workload resources.

The deployer-specific split (FluxCD vs. ArgoCD) is encoded in the Composition itself — each deployer has its own `crossplane-composition.yaml`.

### crossplane-simple

**FluxCD:** `helm/fluxcd/crossplane/simple` — Composition creates an OCM `Resource`, a FluxCD `OCIRepository`, and a `HelmRelease`.

**ArgoCD:** `helm/argocd/crossplane/simple` — Composition creates an OCM `Resource` and an ArgoCD `Application`.

Baseline Crossplane example. Equivalent in scope to `helm-simple` but uses a Crossplane Composition instead of a kro RGD.

```bash
task test/e2e -- -ginkgo.focus=helm/fluxcd/crossplane/simple
```

---

### crossplane-nested

**FluxCD:** `helm/fluxcd/crossplane/nested` | **ArgoCD:** `helm/argocd/crossplane/nested`

The Helm chart lives inside a nested OCM component reference. The OCM `Resource` in the Composition uses `referencePath` to navigate the nesting:

```yaml
referencePath:
  - name: nested-chart
```

---

### crossplane-nested-signed

**FluxCD:** `helm/fluxcd/crossplane/nested-signed` | **ArgoCD:** `helm/argocd/crossplane/nested-signed`

Like `crossplane-nested`, but the component version is signed. The `Resource` controller verifies the signature before exposing OCI coordinates to the Composition pipeline.

---

### crossplane-signing

**FluxCD:** `helm/fluxcd/crossplane/signing` | **ArgoCD:** `helm/argocd/crossplane/signing`

Signature verification for a top-level (non-nested) component. The test signs the component version at setup time; the controller verifies it at runtime.

---

### crossplane-simple-nested-status

**FluxCD:** `helm/fluxcd/crossplane/simple-nested-status` | **ArgoCD:** `helm/argocd/crossplane/simple-nested-status`

Uses `additionalStatusFields` to surface OCI coordinates (registry, repository, digest, tag) from a nested component reference. The Composition reads these fields from the OCM `Resource` status and passes them to the deployer.

---

### crossplane-configuration-localization

**FluxCD:** `helm/fluxcd/crossplane/configuration-localization` | **ArgoCD:** `helm/argocd/crossplane/configuration-localization`

Combines image localization and Helm value injection inside a Composition:

- **Localization** — `image.repository` and `image.tag` are rewritten to the transferred OCI image location.
- **Configuration** — `ui.message` is injected as a Helm value.

FluxCD delivers values via `HelmRelease.spec.values`. ArgoCD uses `Application.spec.source.helm.parameters`.

---

### crossplane-simple-fnc-kro

**FluxCD only:** `helm/fluxcd/crossplane/simple-fnc-kro`

Uses the `function-kro` Crossplane function to compose a kro `ResourceGraph` inside a Composition pipeline. The Composition calls `crossplane-contrib-function-kro`, which renders an OCM `Resource`, a FluxCD `OCIRepository`, and a `HelmRelease` from a `ResourceGraph` template. `function-auto-ready` marks the XR ready once all composed resources are healthy.

This combines two composition engines: Crossplane manages the XR lifecycle; kro renders the resource graph inside the Composition step.

```bash
task test/e2e -- -ginkgo.focus=helm/fluxcd/crossplane/simple-fnc-kro
```

---

## Running all examples

```bash
# Run every example in the generic loop (applyset-pruning is excluded — it has its own test)
task test/e2e

# Tear down, rebuild, and run all examples from scratch
task test/e2e/fresh

# Run a single kro-based example
task test/e2e -- -ginkgo.focus=helm-configuration-localization

# Run a single Crossplane example
task test/e2e -- -ginkgo.focus=helm/fluxcd/crossplane/simple

# Run all Crossplane examples
task test/e2e -- -ginkgo.focus=crossplane
```

See [Taskfile.yml](../Taskfile.yml) for the full list of e2e tasks and options.
