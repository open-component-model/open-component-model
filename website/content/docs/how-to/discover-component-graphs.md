---
title: "Discover Component Graphs"
description: "Publish a filtered, projected view of a Component's transitive reference graph with the Discovery controller using CEL selectors and extraction."
icon: "🔎"
weight: 38
toc: true
---

## Goal

Use a `Discovery` resource to resolve the transitive component graph of a
`Component`, filter it with selectors, and publish either the filtered raw
descriptors or projected free-form records into the `Discovery` status.

## You'll end up with

- A `Discovery` resource that watches a `Component` and publishes a filtered
  view of its reference graph in `status.components` or `status.extracted`.

**Estimated time:** ~10 minutes

## Prerequisites

- [Controller environment]({{< relref "setup-controller-environment.md" >}}) set up
- A `Ready` [Component]({{< relref "docs/reference/kubernetes-api/component.md" >}})
  in the same namespace whose component version references other components

## How it works

A `Discovery` references a `Component` in the same namespace. Once that
`Component` is `Ready`, the controller resolves the **entire** reachable graph
from its resolved repository, applies the reference, component, and resource
selectors, and publishes filtered raw v2 descriptors in `status.components` —
or, when `spec.extract` is set, CEL-projected records in `status.extracted`.

Traversal is full and fail-fast: the first resolution failure retains the last
successful payload and sets `Ready=False`. For selector clauses, extraction
modes, status and scheduling semantics, see
[Component Discovery]({{< relref "docs/concepts/component-discovery.md" >}}).

## Steps

{{< steps >}}
{{< step >}}

### Publish the filtered descriptors

Create a `Discovery` that keeps every component in the graph carrying a matching
label. Without `spec.extract`, the filtered raw v2 descriptors are published in
`status.components`.

```bash
cat <<EOF > discovery.yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Discovery
metadata:
  name: platform-components
  namespace: default
spec:
  componentRef:
    name: releasechannel
  componentSelector:
    matchLabels:
      tier: platform
EOF
```

```bash
kubectl apply -f discovery.yaml
```

{{< /step >}}

{{< step >}}

### Confirm the Discovery is ready

```bash
kubectl get discovery platform-components -o wide
```

Check that the graph was resolved and the observed generation matches:

```bash
kubectl get discovery platform-components -o jsonpath='{.status.conditions[?(@.type=="Ready")].status} {.status.observedGeneration} {.metadata.generation}{"\n"}'
```

Both must hold before you consume the payload: a failed Discovery retains its
last successful result, so `Ready=True` alone does not mean the payload is
current.

{{< /step >}}

{{< step >}}

### Project the graph into records

To publish free-form records instead of raw descriptors, add `spec.extract`.
Each extraction mode returns a **list of objects**. Set exactly one of
`byResources`, `byComponents`, or `expression`.

```bash
cat <<EOF > discovery.yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Discovery
metadata:
  name: flux-images
  namespace: default
spec:
  componentRef:
    name: releasechannel
  componentSelector:
    matchLabels:
      tier: platform
  resourceSelector:
    expression: identity.name in ["flux", "image-automation-controller"]
  extract:
    byResources:
      imageRef: resource.access.imageReference
      resourceName: resource.name
      componentName: component.name
      componentVersion: component.version
EOF
```

```bash
kubectl apply -f discovery.yaml
```

The projected records appear in `status.extracted`:

```bash
kubectl get discovery flux-images -o jsonpath='{.status.extracted}' | jq
```

{{< /step >}}
{{< /steps >}}

## Troubleshooting

Failures surface on the `Ready` condition, with the reason naming the stage that
failed:

```bash
kubectl get discovery platform-components -o jsonpath='{.status.conditions[?(@.type=="Ready")]}' | jq
```

### Symptom: `Ready=False` with reason `ResolutionFailed`

**Cause:** The component graph could not be fully resolved — a repository, auth
or network failure, or a referenced component version that does not exist. A
`Ready` `Component` only guarantees the root descriptor was fetched, not that
the whole graph is reachable.

**Fix:** Confirm every referenced component version exists in a reachable
repository and that credentials cover each host. This is retried with backoff
(up to five minutes between attempts), so fixing a `Secret` revives the object
without a spec change.

### Symptom: `Ready=False` with reason `SelectorFailed` or `ExtractFailed`

**Cause:** A selector or extraction expression failed to compile or evaluate.
The most common case by far is calling `size()` on a field that serialized as
`null` rather than being omitted.

**Fix:** Guard optional reads, for example
`component.componentReferences == null ? 0 : size(component.componentReferences)`.
These are terminal (`Stalled=True`) and are not requeued, so the object needs a
spec change to recover. See
[Absent, null, and missing]({{< relref "docs/concepts/component-discovery.md#absent-null-and-missing" >}}).

### Symptom: `Ready=False` with reason `PayloadTooLarge`

**Cause:** The computed payload exceeds the 1MiB status limit.

**Fix:** Narrow the selectors, or switch to `spec.extract` and project only the
fields you need instead of publishing raw descriptors.

### Symptom: nothing happens after applying the `Discovery`

**Cause:** The referenced `Component` is not `Ready` yet, or the object is
suspended. Discovery is watch-driven with no interval.

**Fix:** Check the `Component` and `spec.suspend`:

```bash
kubectl get component releasechannel -o wide
kubectl get discovery platform-components -o jsonpath='{.spec.suspend}{"\n"}'
```

## Next Steps

- [Verify Component Versions in the Controller]({{< relref "verify-component-version-controller.md" >}}) -
  Verify component version signatures on reconciliation

## Related Documentation

- [Concept: Component Discovery]({{< relref "docs/concepts/component-discovery.md" >}}) -
  Selectors, extraction, status semantics, and scheduling in detail
- [Concept: Kubernetes Controllers]({{< relref "docs/concepts/ocm-controllers.md#discovery" >}}) -
  How Discovery fits alongside the reconciliation chain
- [Reference: Discovery CRD]({{< relref "docs/reference/kubernetes-api/discovery.md" >}}) -
  Full field reference for the Discovery resource
- [How-To: Configure Credentials for OCM Controllers]({{< relref "docs/how-to/configure-credentials-ocm-controllers.md" >}}) -
  Set up registry credentials for the controller
