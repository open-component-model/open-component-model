---
title: "Pack"
description: "Understand how OCM components are composed, identified, and stored."
weight: 10
sidebar:
  collapsed: false
---

Packing is the first step in the OCM lifecycle: it turns software artefacts into a versioned,
self-describing component that can be signed, transferred, and deployed unchanged across any
environment. OCM defines how a component version is identified, where its resources are stored,
how credentials are resolved, and what extensions are available through plugins.

- [Component Identity]({{< relref "docs/concepts/pack/component-identity.md" >}}) — name, version, and provider fields that uniquely identify a component version
- [Canonical Component Repositories]({{< relref "docs/concepts/pack/canonical-components.md" >}}) — how OCM maps component versions to OCI registries
- [Resource Repositories]({{< relref "docs/concepts/pack/resource-repositories.md" >}}) — where resources live and how OCM locates them
- [Credential System]({{< relref "docs/concepts/pack/credential-system.md" >}}) — how credentials are resolved from consumer identities and config
- [Plugin System]({{< relref "docs/concepts/pack/plugin-system.md" >}}) — extending OCM with custom access types, uploaders, and downloaders
- [Software Bills of Materials]({{< relref "docs/concepts/pack/sboms.md" >}}) — attaching SBOMs as typed resources inside a component version
