---
title: "Ownership"
description: "How OCM traces a resource back to the component version that owns it, without modifying the resource."
weight: 9
toc: true
hasMermaid: true
---

Ownership is about tracing a resource back to its owning component version. Find a resource in a registry and it
looks like any other image: nothing on it points back to the component version it belongs to. The component version
lists its resources, but the resources carry no information about the component version.

OCM's **ownership tracking** adds that missing reverse link. Given a resource, it lets you find the component version
that owns it.

## Always the Same Record

An ownership record carries no timestamp or other varying data, so its content is fully determined by the resource
and the owner. The same resource owned by the same component version always produces the **same** ownership record. Re-running
`ocm add component-version` lands on that same record; the registry sees a duplicate and keeps just one. A resource
ends up with exactly **one** ownership record, however many times it is created.

## Ownership Across Transfer

When you transfer a component version to another registry, OCM brings the ownership records of **by-value**
resources along automatically.

For the full picture of how artifacts move between repositories, see [Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}}).

## What's Next?

- [How-To: Add and Verify Ownership Information]({{< relref "docs/how-to/add-and-verify-ownership.md" >}}) — opt a
  resource into ownership tracking and verify the ownership record.

## Related Documentation

- [Concept: Resource Repositories]({{< relref "docs/concepts/resource-repositories.md" >}}) — how OCM stores and
  authenticates against the registries where resources (and their ownership records) live.
- [Concept: Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}}) — how component versions and
  their resources move between repositories.
- [Concept: Signing and Verification]({{< relref "docs/concepts/signing-and-verification-concept.md" >}}) — how OCM
  establishes the authenticity of a component version.
- [ADR 0016: Ownership Annotations](https://github.com/open-component-model/open-component-model/blob/main/docs/adr/0016_ownership_annotations.md)
  — the design decision and the alternatives that were considered.
