---
title: Replication
description: "API reference for the Replication custom resource (delivery.ocm.software/v1alpha1)"
weight: 5
toc: false
---

A **Replication** transfers a component version from one OCM repository to
another, the same behaviour of `ocm transfer` as a controller. It references
a `Component` for the source version and a target `Repository`, and re-runs the
transfer whenever the resolved source version changes.

---

## API Specification

{{< schema-renderer url="/schemas/kubernetes/controller/delivery.ocm.software_replications.yaml" >}}
