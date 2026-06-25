---
title: Controller CRDs
description: "API reference for OCM Kubernetes Custom Resources (delivery.ocm.software/v1alpha1)"
weight: 7
toc: true
sidebar:
  collapsed: true
---

This section documents the Custom Resource Definitions (CRDs) provided by the
OCM Kubernetes controller. All resources belong to the API group
**`delivery.ocm.software/v1alpha1`**.

| Kind                                        | Scope      | Description                                                  |
|---------------------------------------------|------------|--------------------------------------------------------------|
| [Repository]({{< relref "repository" >}})   | Namespaced | Represents an OCM repository to be validated                 |
| [Component]({{< relref "component" >}})     | Namespaced | Tracks an OCM component version from a repository            |
| [Resource]({{< relref "resource" >}})       | Namespaced | References a specific resource within a component version    |
| [Deployer]({{< relref "deployer" >}})       | Cluster    | Deploys OCM resources into the cluster                       |
| [Replication]({{< relref "replication" >}}) | Namespaced | Replicates an OCM component version into a target repository |
