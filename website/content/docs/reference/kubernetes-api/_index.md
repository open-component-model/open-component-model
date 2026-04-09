---
title: Controller CRDs
description: "API reference for OCM Kubernetes Custom Resources (delivery.ocm.software/v1alpha1)"
weight: 6
toc: true
sidebar:
  collapsed: true
---

This section documents the Custom Resource Definitions (CRDs) provided by the
OCM Kubernetes controller. All resources belong to the API group
**`delivery.ocm.software/v1alpha1`**.

| Kind | Scope | Description |
|---|---|---|
| [Component]({{< relref "component" >}}) | Namespaced | Tracks an OCM component version from a repository |
| [Repository]({{< relref "repository" >}}) | Namespaced | Represents an OCM repository to be validated |
| [Resource]({{< relref "resource" >}}) | Namespaced | References a specific resource within a component version |
| [Deployer]({{< relref "deployer" >}}) | Cluster | Deploys OCM resources into the cluster |
