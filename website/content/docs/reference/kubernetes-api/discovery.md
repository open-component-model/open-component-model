---
title: Discovery
description: "API reference for the Discovery custom resource in delivery.ocm.software/v1alpha1, which publishes a filtered view of a Component's reference graph."
weight: 6
toc: false
---

A **Discovery** publishes a filtered, optionally projected view of the transitive
reference graph of a `Component`. It resolves the entire reachable graph from the
`Component`'s resolved repository, applies reference, component, and resource
selectors, and either publishes the filtered raw v2 descriptors or projects them
into free-form records with CEL expressions.

Discovery is read-only: it creates no external resources, downloads no artifacts,
and provides no signature-verification guarantees for the descriptors it filters.
See the [Discover Component Graphs]({{< relref "docs/how-to/discover-component-graphs.md" >}})
how-to guide for usage, bindings, and status semantics.

---

## API Specification

{{< schema-renderer url="/schemas/kubernetes/controller/delivery.ocm.software_discoveries.yaml" >}}
