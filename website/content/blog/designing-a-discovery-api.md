---
title: "Component Discovery - the Missing Kubernetes Primitive"
description: "A new Discovery API for the OCM Kubernetes controllers that makes component graphs queryable from within a cluster."
date: 2026-07-23T10:00:00+02:00
contributors: []
tags: ["Kubernetes", "Controller", "discovery", "supply-chain"]
draft: false
---

## The Manual Grind

Platforms like [OpenControlPlane](https://github.com/openmcp-project) run service providers that install and manage services (Flux, Crossplane, Kro, ...) on behalf of their users. Each service provider needs to know which versions of a service are available and where to fetch the corresponding artifacts from. Today, that means hardcoding OCI references (image URLs, chart registries, pull secrets) into provider configs, per version, per service:

```yaml
apiVersion: flux.services.open-control-plane.io/v1alpha1
kind: ProviderConfig
metadata:
  name: flux
spec:
  versions:
    - version: "2.8.3"
      chartVersion: "2.18.2"
      chartUrl: "oci://ghcr.io/fluxcd-community/charts/flux2"
      chartPullSecret: "chart-registry-credentials"
      values:
        helmController:
          image: my-registry.example.com/fluxcd/helm-controller
          tag: v1.5.3
        sourceController:
          image: my-registry.example.com/fluxcd/source-controller
          tag: v1.8.1
    - version: "2.18.2"
      chartVersion: "2.18.2"
      chartUrl: "oci://ghcr.io/fluxcd-community/charts/flux2"
      chartPullSecret: "chart-registry-credentials"
      values:
        helmController:
          image: my-registry.example.com/fluxcd/helm-controller
          tag: v1.5.3
        sourceController:
          image: my-registry.example.com/fluxcd/source-controller
          tag: v1.8.1
    # ... repeated per version
```

Add a version? Edit YAML. Switch registries for an air-gapped environment? Edit more YAML. Ask "what's available?" Grep and hope.

OCM (Open Component Model) already models this: components reference other components, components carry resources (images, charts), the whole graph is transportable. But there was no way to **query it from inside a cluster**. Controllers couldn't ask "what versions of Flux exist?" or "give me all images for Kro v0.9.2."

---

## The Idea: A Service Catalog as a Component Graph

Instead of scattering artifact locations across controller configs, model your entire service catalog as an OCM component tree. An umbrella component references all available services and versions:

```yaml
- name: ocm.software/service-catalog
  version: "1.0.0"
  componentReferences:
    - name: flux
      componentName: ocm.software/service-catalog/flux
      version: "2.8.3"
    - name: flux
      componentName: ocm.software/service-catalog/flux
      version: "2.18.2"
    - name: ocm
      componentName: ocm.software/kubernetes/controller
      version: "0.9.0"
    - name: ocm
      componentName: ocm.software/kubernetes/controller
      version: "0.10.0"
    - name: ocm
      componentName: ocm.software/kubernetes/controller
      version: "0.11.0"
    - name: kro
      componentName: ocm.software/service-catalog/kro
      version: v0.9.0
    - name: kro
      componentName: ocm.software/service-catalog/kro
      version: v0.9.1
    - name: kro
      componentName: ocm.software/service-catalog/kro
      version: v0.9.2
```

Each referenced component carries its actual artifacts:

```yaml
- name: ocm.software/service-catalog/kro
  version: v0.9.2
  resources:
    - name: kro
      type: helmChart
      access:
        type: ociArtifact
        imageReference: registry.k8s.io/kro/charts/kro:0.9.2
    - name: image-kro
      type: ociImage
      access:
        type: ociArtifact
        imageReference: registry.k8s.io/kro/kro:v0.9.2
```

Full service-catalog example: [gist](https://gist.github.com/frewilhelm/897409012f8a30daec2314249cdf1f80)

The structure is there. What was missing: a way to **query it declaratively from within the cluster**. So we built a controller that makes this graph queryable via a single Kubernetes resource.

---

## The PoC: A Discovery Resource

We introduced a new CRD: `Discovery`. Point it at a `Component` object, declare selectors, and the controller resolves the graph for you.

### Example 1: Full Component Descriptors

"Give me all Kro versions >= v0.9.1", returning the complete OCM descriptors:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Discovery
metadata:
  name: kro-versions
spec:
  componentRef:
    name: service-catalog
  referenceSelector:
    matchIdentity:
      name: "ocm.software/service-catalog/kro"
    matchExpressions:
      - key: version
        operator: SemverRange
        values: [">= 0.9.1"]
```

**Result in `status.discovery`:**

```json
[
  {
    "component": {
      "name": "ocm.software/service-catalog/kro",
      "provider": "ocm.software",
      "version": "v0.9.1",
      "resources": [
        {
          "name": "kro",
          "type": "helmChart",
          "access": {
            "type": "ociArtifact",
            "imageReference": "registry.k8s.io/kro/charts/kro:0.9.1@sha256:37b00031..."
          },
          "digest": { "hashAlgorithm": "SHA-256", "value": "37b00031..." }
        },
        {
          "name": "image-kro",
          "type": "ociImage",
          "access": {
            "type": "ociArtifact",
            "imageReference": "registry.k8s.io/kro/kro:v0.9.1@sha256:f460b2d7..."
          },
          "digest": { "hashAlgorithm": "SHA-256", "value": "f460b2d7..." }
        }
      ]
    },
    "meta": { "schemaVersion": "v2" }
  },
  {
    "component": {
      "name": "ocm.software/service-catalog/kro",
      "provider": "ocm.software",
      "version": "v0.9.2",
      "resources": ["..."]
    },
    "meta": { "schemaVersion": "v2" }
  }
]
```

Full descriptors with digests and access info. Everything your controller needs to verify and pull.

### Example 2: Projected Fields with discoveryFields

Same query, but extracting only what you need into a flat, consumable format:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Discovery
metadata:
  name: kro-charts
spec:
  componentRef:
    name: service-catalog
  referenceSelector:
    matchIdentity:
      name: "ocm.software/service-catalog/kro"
    matchExpressions:
      - key: version
        operator: SemverRange
        values: [">= 0.9.1"]
  resourceSelector:
    matchIdentity:
      name: kro
  discoveryFields:
    version: "component.version"
    chart: "resource.access.imageReference"
```

**Result:**

```json
[
  {
    "chart": "registry.k8s.io/kro/charts/kro:0.9.1@sha256:37b00031...",
    "version": "v0.9.1"
  },
  {
    "chart": "registry.k8s.io/kro/charts/kro:0.9.2@sha256:bafeda52...",
    "version": "v0.9.2"
  }
]
```

Two fields, no noise. Ready to be consumed by any downstream controller without OCM-specific logic.

---

## How It Works

The `Discovery` controller:

1. **Resolves the root component** from its OCI repository. Credentials flow through the existing OCM config chain.
2. **Traverses all `componentReferences` recursively** using OCM's DAG library, resolving nested trees in parallel.
3. **Applies `referenceSelector`** to filter which referenced components match.
4. **Applies `resourceSelector`** to filter resources within each matched component.
5. **Projects via `discoveryFields`** using dot-notation paths (`resource.access.imageReference`, `component.version`) to extract exactly what consumers need into a flat array.

Both selectors follow the same pattern as Kubernetes label selectors (`matchLabels`, `matchExpressions` with `In`, `NotIn`, `Exists`, `DoesNotExist`), extended with `matchIdentity` and a `SemverRange` operator.

The Discovery controller watches the referenced `Component` CR. When the component version changes (e.g. because the Component CR is configured with an interval or a semver constraint that picks up a new release), the Discovery re-reconciles automatically.

---

## What's Next

This is a proof of concept. Open questions and next steps:

- **Efficiency.** Currently the controller downloads the entire component graph on every reconciliation, which is overkill. Potential improvements include leveraging the blob cache, a metadata informer that avoids full descriptor downloads, and introducing recursiveness when walking reference paths.
- **Signature verification of references.** The root component is already verified by the `Component` CR, but referenced components are not yet verified individually. The trade-off: verifying each reference means downloading the descriptor and computing a checksum, which may be too expensive for large graphs.
- **CEL expressions.** Upgrading from simple dot-path extraction to CEL for richer projections and conditional logic.

---

Upstream issue: [open-component-model/ocm-project#1153](https://github.com/open-component-model/ocm-project/issues/1153)
