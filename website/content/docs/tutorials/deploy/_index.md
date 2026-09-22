---
title: "Deploy"
description: "Tutorials for deploying OCM component versions with controllers and GitOps tooling."
weight: 40
sidebar:
  collapsed: false
---

During the deploy phase you turn a component version into a running workload. These tutorials
walk through complete end-to-end deployments using OCM controllers together with Flux and Kro.

- [Deploy an Application from a Helm Chart with OCM and Kro]({{< relref "docs/tutorials/deploy/deploy-helm-chart-bootstrap.md" >}}) — bootstrap a Kubernetes cluster, push a Helm chart as a component version, and deploy it with Flux and Kro
- [Deploy an Application from Chained RGDs with OCM and Kro]({{< relref "docs/tutorials/deploy/deploy-chained-rgds.md" >}}) — chain multiple Resource Group Definitions to compose and deploy a multi-component application
