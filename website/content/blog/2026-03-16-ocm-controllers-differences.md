---
title: "What changed between legacy and new OCM Controllers"
date: 2026-03-16T10:00:00+01:00
draft: false
description: "Learn about the differences in concept and architecture of legacy and new OCM Controllers"
summary: "A walkthrough of the major architectural changes between the legacy ocm-controller and the new ground-up rebuild."
weight: 50
categories: ["controllers"]
tags: ["kubernetes"]
contributors: []
---

The [new controller](https://github.com/open-component-model/open-component-model/tree/main/kubernetes/controller) is the ground-up rebuild of [legacy](https://github.com/open-component-model/ocm-controller/) controller. It is simpler to operate, has fewer moving parts, and removes the need for external dependencies like Flux. This guide walks through the major differences between the legacy controller and new controller and what they mean for you.

---

## At a Glance

The number of custom resources dropped from **7 to 4**, and the overall architecture is leaner:

| Previous | New | What happened |
|---|---|---|
| `ComponentVersion` | `Component` | Renamed and restructured |
| `ComponentDescriptor` | — | Removed |
| `Resource` | `Resource` | Redesigned |
| `Snapshot` | — | Removed |
| `Configuration` | — | Removed |
| `Localization` | — | Removed |
| `FluxDeployer` | `Deployer` | Replaced |
| — | `Repository` | New |

The new dependency chain is: **Repository → Component → Resource → Deployer**. Each layer depends only on the one above it.

---

## Repository

Previously, repository connection details (registry URL, credentials) were defined inline on every `ComponentVersion`. If you had ten components from the same registry, you repeated the same configuration ten times.

The new `Repository` resource changes that. You define your registry connection once, and all your Components reference it by name. The controller validates that the repository is reachable and healthy on its own schedule. It also prevents accidental deletion, you cannot remove a Repository while Components still reference it.

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Repository
metadata:
  name: my-registry
  namespace: ocm-system
spec:
  repositorySpec:
    type: OCIRegistry
    baseUrl: ghcr.io/my-org
  ocmConfig:
    - kind: Secret
      name: registry-credentials
  interval: 10m
```

The `ocmConfig` entries support both Secrets and ConfigMaps. The `policy` field controls whether configuration is inherited by child resources. `Propagate` is the default, set it to `DoNotPropagate` if the credentials should not flow downstream.

Multiple Components can now reference this single Repository by name, keeping credentials and connection details in one place.

---

## ComponentVersion is now Component

The resource formerly known as `ComponentVersion` has been renamed to `Component` and gained several new capabilities.

**What stayed the same:** You still specify a component name, a semver version constraint, signature verification, and a reconciliation interval.

**What changed:**

- **Repository is now a reference.** Instead of embedding registry details inline, you point to a `Repository` resource. This keeps your Component definitions clean and your credentials in one place.

- **Downgrade policy.** The previous controller always moved forward to the latest matching version. The new controller lets you choose:
  - *Deny* (default): never downgrade.
  - *Allow*: permit downgrades, but only if the component is explicitly labeled as downgradable.
  - *Enforce*: always allow downgrades, no questions asked.

- **Async resolution.** Version lookups no longer block the reconciliation loop. The controller hands off resolution to a background worker pool and picks up the result when it's ready. This makes the controller more responsive when dealing with slow or large registries. The component is updated via an event queue once resolution finishes. This means, no `RequeueAfter` is required for that resolution to be picked up by the component again.

Here is an example showing the new Component with signature verification and a downgrade policy:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Component
metadata:
  name: my-component
  namespace: ocm-system
spec:
  repositoryRef:
    name: my-registry
  component: github.com/my-org/my-app
  semver: ">=1.0.0"
  downgradePolicy: Allow
  verify:
    - signature: ocm.software
      secretRef:
        name: signing-key
  # ocmConfig: # this is now taken from the `repositoryRef` since that object already contains this configuration.
  #   - kind: Secret
  #     name: registry-credentials
  #     policy: Propagate
  interval: 10m
```

The verification secret contains the public key used to validate the component's signature:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: signing-key
  namespace: ocm-system
data:
  ocm.software: <base64-encoded PEM public key>
```

The key name in the Secret's `data` field must match the `signature` value in the Component's `verify` list.

**What was removed:**

- **Component transfer.** The old `destination` field let you mirror a component to a secondary registry during reconciliation. This feature is no longer built in, use an external mirroring tool if needed.

- **ComponentDescriptor resources.** The old controller created a separate `ComponentDescriptor` custom resource for every component and its transitive references. These are gone. The resolved descriptor data now lives directly in the Component's status, which means fewer resources in your cluster and less noise when listing objects.

---

## Resource

The `Resource` still represents a specific artifact (a Helm chart, a set of manifests, an image) within a component. But the internals have changed significantly.

**Key changes:**

- **No more Snapshots.** The old controller extracted each resource into a `Snapshot`, which was stored in a local OCI registry running inside the cluster. That entire layer with the Snapshot custom resource, the in-cluster registry and the caching logic, is gone. Resources are now fetched on-demand when needed, with an in-memory cache to avoid redundant downloads.

- **Optional digest verification.** By default, the controller verifies the integrity of every resource it fetches. The new `VerificationPolicy` option lets you skip this step for resources where the download cost is high and you trust the source. Use with care.

- **Custom status fields via CEL expressions.** You can define expressions that extract values from resource metadata and surface them in the Resource's status without writing a custom controller.

- **Namespace-local references only.** The old controller allowed cross-namespace references. The new controller requires that a Resource and its parent Component live in the same namespace.

Here is a Resource with CEL-based custom status fields:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Resource
metadata:
  name: my-helm-chart
  namespace: ocm-system
spec:
  componentRef:
    name: my-component
  resource:
    byReference:
      resource:
        name: helm-chart
  additionalStatusFields:
    registry: resource.access.imageReference.toOCI().registry
    repository: resource.access.imageReference.toOCI().repository
    tag: resource.access.imageReference.toOCI().tag
    digest: resource.access.imageReference.toOCI().digest
```

The CEL expressions run against the resolved OCM resource descriptor. The built-in `toOCI()` function parses OCI image references into their components (`host`, `registry`, `repository`, `reference`, `tag`, `digest`). You can also access any field on the resource directly. For example, `resource.access.repoUrl` or `resource.access.helmChart`.

The results appear in the Resource's status:

```yaml
status:
  additional:
    registry: "ghcr.io"
    repository: "my-org/my-app/charts/my-helm-chart"
    tag: "1.2.0"
    digest: "sha256:7a91508d..."
```

For resources nested behind component references, use `referencePath`:

```yaml
spec:
  resource:
    byReference:
      resource:
        name: deploy-chart
      referencePath:
        - name: nested-component
```

---

## FluxDeployer is now Deployer

This is the biggest architectural shift.

The old `FluxDeployer` didn't actually deploy anything itself. It created Flux resources, an `OCIRepository` plus either a `Kustomization` or a `HelmRelease`, and let Flux handle the actual deployment. This meant your cluster needed the full Flux suite installed and running.

The new `Deployer` handles deployment directly. It downloads the manifests from the OCM resource and applies them to the cluster using Kubernetes server-side apply with ApplySet tracking. No Flux required.

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Deployer
metadata:
  name: my-deployer
spec:
  resourceRef:
    name: my-manifests
    namespace: ocm-system
```

That's it. No templates, no intermediate resources. The Deployer fetches the manifests from the referenced Resource and applies them.

**What this means in practice:**

- **One fewer dependency.** You no longer need Flux installed in your cluster for OCM-based delivery to work. The controller is fully self-contained.

- **Automatic cleanup.** When manifests are removed from a resource, the Deployer automatically prunes the corresponding objects from the cluster. This uses the Kubernetes ApplySet mechanism, which tracks which objects belong to which Deployer.

- **Drift detection.** The Deployer watches the objects it has deployed. If something changes them externally, the controller notices and can reconcile the drift.

**What was removed:**

- **Kustomization and Helm support.** The old controller could template deployments through Flux's Kustomization or HelmRelease. The new Deployer applies raw manifests only. If you need Kustomize overlays or Helm rendering, do that before publishing your component. The controller expects ready-to-apply YAML.

---

## Configuration and Localization are gone

The old controller provided two mutation resources: `Configuration` (for applying value overrides and patches) and `Localization` (for applying environment-specific transformations). Both operated on Snapshots and produced new Snapshots with the modifications applied.

These resources no longer exist. The new controller takes the position that mutations should happen at build time, not deploy time. Prepare your manifests with the right configuration and localization before publishing them as OCM resources, and the controller will deliver them as-is. Otherwise, you can use operators like [Kro](https://github.com/kubernetes-sigs/kro/) and its `ResourceGraphDefinition` to do certain operations. To read up more about how to do things with Kro and use it to deploy applications, please check the following document: [Deploy Helm Chart]({{< relref "docs/getting-started/deploy-helm-chart.md" >}}).

---

## Infrastructure Requirements

The operational footprint is significantly smaller:

| | Previous | New |
|---|---|---|
| In-cluster OCI registry | Required (for Snapshot caching) | Not needed |
| Flux controllers | Required (for deployment) | Not needed |
| Performance tuning | No exposed knobs | Configurable concurrency, cache sizes, worker counts |

The removal of the local OCI registry alone simplifies operations, that's one fewer stateful component to monitor, back up, and secure with TLS certificates.

---

## Conclusion

The previous controller orchestrated a multi-step pipeline: fetch, snapshot, configure, localize, then hand off to Flux for deployment. The new controller is a direct delivery mechanism: fetch, verify, apply. It does less, but what it does, it does with fewer moving parts and fewer things that can break.
