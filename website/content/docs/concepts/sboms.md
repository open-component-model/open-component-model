---
title: "Software Bills of Materials"
description: "What is an SBOM and its relevance to the OCM ecosystem."
icon: "🧾"
weight: 10
toc: true
---

A Software Bill of Materials is an inventory of what is inside an artifact: packages, libraries and versions. It is what
a scanner reads to show what CVEs might affect the current items. To read a full description, please read the 
[National Telecommunications and Information Administration's SBOM Page](https://www.ntia.gov/SBOM).

## Why OCM Cares

A component version already tells you **which** artifacts you deliver. It does not tell you **what is inside** of them.
Those two items are usually defined in two different places and by different tools.

OCM creates a link between these two. Generally speaking, we get three things out of defining SBOMs in a component
version:

- **It is identified.** The SBOM is bound to the resource it describes by an identity, not by a file name or a tag. This
                        creates a hard link from an SBOM to an actual resource that is easy to follow and understand.
- **It is signed.** This link is a label and if `signing: true` is set on it, it means it will be part of the signature.
                    That means, that now it's immutable and any change will require a signature update.
- **It goes where the resource goes.** Transfer the component version and the SBOM is transferred with it, including
                                       into an air-gapped environment and so the SBOM remains useful.

## Where SBOMs Actually Reside

In practice, they are in many places. Some you generate yourself, with a tool like `syft` or `trivy`. Some, are already
attached to an image by the build system, as BuildKit does with `docker buildx build --sbom=true`. Some do not exist at
all.

OCM does not force you to normalize this up front. `ocm download resource --sbom` looks for a linked SBOM resource
first and falls back to reading what is attached to the artifact. To understand how that works, please read our SBOM
tutorial at [Working with SBOMs]({{< relref "docs/tutorials/working-with-sboms.md" >}}).

## What's Next?

- [Tutorial: Working with SBOMs]({{< relref "docs/tutorials/working-with-sboms.md" >}}) — understand how SBOMs are 
  attached to component versions or discovered.

## Related Documentation

- [Concept: Signing and Verification]({{< relref "docs/concepts/signing-and-verification-concept.md" >}}) — what makes
  a linked SBOM trustworthy.
- [Concept: Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}}) — how component versions and
  their resources move between repositories.
- [Concept: Component Identity]({{< relref "docs/concepts/component-identity.md" >}}) — the identity an SBOM link
  points at.
