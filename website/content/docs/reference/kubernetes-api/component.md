---
title: Component
description: "API reference for the Component custom resource (delivery.ocm.software/v1alpha1)"
weight: 1
toc: false
---

A **Component** represents an OCM component version to be tracked by the controller.
It references a `Repository` object and specifies a semantic version constraint to
select which component version to reconcile.

---

## API Specification

{{< schema-renderer url="/schemas/kubernetes/controller/delivery.ocm.software_components.yaml" >}}
