# Flux ExternalArtifact integration example

This example demonstrates the `--enable-flux-external-artifacts-api` feature of
the OCM controller (RFC-0012). When enabled, the controller produces a Flux
`ExternalArtifact` (`source.toolkit.fluxcd.io/v1`) for **every** OCM `Resource`,
using the same name and namespace as the Resource.

Each `ExternalArtifact` reports the packaged OCM content as a `tar.gz` artifact
in its `.status.artifact`, served in-cluster over HTTP, so that the Flux
`kustomize-controller` and `helm-controller` can consume it via a `sourceRef`
or `chartRef` of kind `ExternalArtifact`.

## Enabling the feature

Install (or upgrade) the chart with the feature toggle:

```bash
helm upgrade --install ocm-k8s-toolkit ./chart \
  --set fluxExternalArtifacts.enable=true
```

Or, when running the manager binary directly:

```bash
manager --enable-flux-external-artifacts-api \
  --external-artifact-storage-path=/data \
  --external-artifact-storage-address=:9091 \
  --external-artifact-storage-advertise-address=ocm-k8s-toolkit-controller-manager.ocm-system.svc.cluster.local.:9091
```

## How it works

1. The OCM `Repository`, `Component` and `Resource` objects resolve an OCM
   resource as usual (see `bootstrap.yaml`).
2. Once the `Resource` is `Ready`, the ExternalArtifact controller:
   - downloads the referenced OCM content (a **local blob** or an **OCI
     artifact** carrying a Helm chart or Kustomize overlay),
   - packages it into a `tar.gz` on the controller's local filesystem,
   - serves it over the in-cluster HTTP storage endpoint,
   - creates/updates an `ExternalArtifact` named after the `Resource`, owned by
     it, and records the artifact in `.status.artifact`.
3. A Flux `Kustomization` (for Kustomize overlays / plain manifests) or
   `HelmRelease` (for Helm charts) references the `ExternalArtifact` and applies
   its contents.

## Example generated ExternalArtifact

```yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: ExternalArtifact
metadata:
  name: kustomize-simple-resource-rgd
  namespace: default
spec:
  sourceRef:
    apiVersion: delivery.ocm.software/v1alpha1
    kind: Resource
    name: kustomize-simple-resource-rgd
    namespace: default
status:
  artifact:
    digest: sha256:35d47c9db0eee6ffe08a404dfb416bee31b2b79eabc3f2eb26749163ce487f52
    lastUpdateTime: "2025-08-21T13:37:31Z"
    path: resource/default/kustomize-simple-resource-rgd/35d47c9d.tar.gz
    revision: 1.0.0@sha256:35d47c9db0eee6ffe08a404dfb416bee31b2b79eabc3f2eb26749163ce487f52
    size: 20914
    url: http://ocm-k8s-toolkit-controller-manager.ocm-system.svc.cluster.local.:9091/resource/default/kustomize-simple-resource-rgd/35d47c9d.tar.gz
  conditions:
    - lastTransitionTime: "2025-08-21T13:37:31Z"
      message: stored artifact for revision 1.0.0@sha256:35d47c9d...
      observedGeneration: 1
      reason: Succeeded
      status: "True"
      type: Ready
```

## Files

- `bootstrap.yaml` — the OCM `Repository`, `Component` and `Resource` objects.
- `flux-kustomization.yaml` — a Flux `Kustomization` consuming the generated
  `ExternalArtifact` (for a Kustomize/manifest resource).
- `flux-helmrelease.yaml` — a Flux `HelmRelease` consuming the generated
  `ExternalArtifact` (for a Helm chart resource).
