---
title: Deployer
description: "API reference for the Deployer custom resource (delivery.ocm.software/v1alpha1)"
weight: 4
toc: false
---

A **Deployer** is a cluster-scoped resource that deploys OCM resources into the
cluster. It references a `Resource` containing a `ResourceGroupDefinition` and
manages the lifecycle of the deployed Kubernetes objects.

---

## API Specification

{{< schema-renderer url="/schemas/kubernetes/controller/delivery.ocm.software_deployers.yaml" >}}
