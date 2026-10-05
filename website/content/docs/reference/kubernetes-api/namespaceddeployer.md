---
title: NamespacedDeployer
description: "API reference for the NamespacedDeployer custom resource (delivery.ocm.software/v1alpha1)"
weight: 4
toc: false
---

A **NamespacedDeployer** is a namespaced resource that deploys OCM resources. It references a `Resource` in the same
namespace and applies and prunes the deployed objects with the permissions of the service account in
`spec.serviceAccountName`, so that service account's RBAC decides what it may deploy.

---

## API Specification

{{< schema-renderer url="/schemas/kubernetes/controller/delivery.ocm.software_namespaceddeployers.yaml" >}}
