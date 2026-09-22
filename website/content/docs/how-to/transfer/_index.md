---
title: "Transfer"
description: "How-to guides for moving component versions between repositories and environments."
weight: 30
sidebar:
  collapsed: false
---

During the transfer phase you move a component version, together with its resources, to the
registry where it will be deployed — including across air gaps and organisational boundaries.
These guides cover the most common transfer scenarios with the OCM CLI and controllers.

- [Transfer Helm Charts with OCM]({{< relref "docs/how-to/transfer/transfer-helm-charts.md" >}}) — copy a Helm chart bundled in a component version to a target registry
- [Transfer Components across an Air Gap]({{< relref "docs/how-to/transfer/air-gap-transfer.md" >}}) — package a component version into a Common Transport Archive and import it offline
- [Resolve Components across Multiple Registries]({{< relref "docs/how-to/transfer/resolve-components-from-multiple-repositories.md" >}}) — configure resolvers for complex registry topologies
- [Replicate Component Versions with the Controller]({{< relref "docs/how-to/transfer/replicate-component-versions-controller.md" >}}) — automate replication between registries using the OCM controller
- [Configure Credentials for Multiple Registries]({{< relref "docs/how-to/transfer/configure-multiple-credentials.md" >}}) — supply per-registry credentials in the OCM config
- [Migrate Legacy Credentials]({{< relref "docs/how-to/transfer/legacy-credential-compatibility.md" >}}) — update credential configs from the deprecated format
- [Migrate from Fallback to Deterministic Resolvers]({{< relref "docs/how-to/transfer/migrate-from-deprecated-resolvers.md" >}}) — replace deprecated fallback resolvers with explicit glob-based rules
