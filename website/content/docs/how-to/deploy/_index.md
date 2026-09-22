---
title: "Deploy"
description: "How-to guides for deploying OCM component versions into Kubernetes clusters."
weight: 40
sidebar:
  collapsed: false
---

During the deploy phase you turn a component version into a running workload in a Kubernetes
cluster. These guides cover applying manifests, configuring access control, and ensuring
component integrity before rollout.

- [Deploy Manifests with the Deployer]({{< relref "docs/how-to/deploy/deploy-manifests-with-deployer.md" >}}) — apply Kubernetes resources from a component version using the OCM Deployer
- [Verify Component Versions in the Controller]({{< relref "docs/how-to/deploy/verify-component-version-controller.md" >}}) — enforce signature verification before the controller deploys a component version
- [Configure Credentials for OCM Controllers]({{< relref "docs/how-to/deploy/configure-credentials-ocm-controllers.md" >}}) — provide registry credentials to OCM controllers running in a cluster
- [Configure Custom RBAC for Deployers]({{< relref "docs/how-to/deploy/custom-rbac.md" >}}) — restrict deployer permissions to the minimum required for your workloads
