---
title: "Deploy"
description: "Deploy component versions to Kubernetes with the OCM Controllers: credentials, RBAC, discovery, deployers, and signature verification."
icon: "🚀"
weight: 40
sidebar:
  collapsed: true
---

Deploy guides take an OCM component version into a running Kubernetes cluster with the
OCM Controllers. They cover configuring controller credentials and RBAC, discovering
component graphs, deploying manifests and Helm charts (directly, with kro, or through a
GitOps deployer), and verifying component signatures before reconciliation.

New to deploying with the OCM Controllers? Start with
[Deploy Kubernetes Manifests with the OCM Deployer]({{< relref "docs/guides/deploy/deploy-manifests-with-deployer.md" >}})
for the shortest path from a component version to a running workload, then layer on
credentials, RBAC, discovery, and signature verification as your setup needs them.
