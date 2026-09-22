---
title: "Transfer"
description: "Understand how OCM moves component versions across registries, air gaps, and environments."
weight: 30
sidebar:
  collapsed: false
---

Transfer is the third step in the OCM lifecycle: it moves a signed component version, together
with all its resources, from one registry to another — including across air gaps and organisational
boundaries. OCM provides first-class primitives for locating components in complex registry
topologies, packaging them for offline delivery, and tracking which component version owns a
given resource after it has been moved.

- [Transfer and Transport]({{< relref "docs/concepts/transfer/transfer-concept.md" >}}) — copy semantics, by-value vs. by-reference resources, and the Common Transport Format for air-gapped delivery
- [Resolvers]({{< relref "docs/concepts/transfer/resolvers.md" >}}) — how OCM locates component versions across registry hierarchies using glob-based rules
- [Ownership]({{< relref "docs/concepts/transfer/ownership.md" >}}) — the reverse link that lets you trace a resource in a registry back to the component version that owns it
