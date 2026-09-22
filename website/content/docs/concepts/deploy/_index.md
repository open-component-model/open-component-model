---
title: "Deploy"
description: "Understand how OCM controllers deploy component versions into Kubernetes clusters."
weight: 40
sidebar:
  collapsed: false
---

Deployment is the final step in the OCM lifecycle: it turns a verified component version into a
running workload in a Kubernetes cluster. OCM provides a Kubernetes-native delivery model built
around two complementary primitives — controllers that reconcile component versions from a
registry, and a deployer that applies their resources as live cluster objects.

- [Kubernetes Controllers]({{< relref "docs/concepts/deploy/ocm-controllers.md" >}}) — how OCM controllers watch component versions, trigger reconciliation, and integrate with Flux and Kro
- [Kubernetes Deployer]({{< relref "docs/concepts/deploy/kubernetes-deployer.md" >}}) — how the deployer renders and applies manifests from a component version into a cluster
