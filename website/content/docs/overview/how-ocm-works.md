---
title: "How OCM Works"
description: >
  An introduction to the Open Component Model — how it packages, secures,
  transports, and deploys software across any boundary.
weight: 1
---

Modern software doesn't just run — it travels. From a developer's laptop to a CI pipeline,
from a public cloud to a sovereign data center that has no connection to the internet.
Along the way, it crosses organizational boundaries, trust boundaries, and sometimes physical ones.

OCM gives you a single, standard way to handle that journey:
**Pack** your software, **Sign** it for integrity, **Transport** it across any boundary, and **Deploy** it at the target.

## What Is a Component?

Before anything can be packed, signed, or shipped, OCM needs a way to describe your software.
That's what a [component]({{< relref "component-identity.md" >}}) is: a named, versioned collection of everything
that belongs to a deliverable — container images, Helm charts, config files, binaries, and even documentation.

Each component carries a **component descriptor** that acts like a packing list: what's inside, where it came from,
and how to access it. This descriptor is the foundation that makes signing, transport, and deployment work.

The beauty is that OCM doesn't care what kind of artifacts you're shipping. OCI images, Terraform modules, binaries, or documentation  —
they all get the same uniform treatment.

## Pack

Packing means describing your artifacts in a component descriptor and storing them in a repository —
an OCI registry, a file archive, or anything OCM supports.

You're not converting or re-packaging your artifacts. You're wrapping them in a standard envelope
so that the rest of the pipeline knows exactly what it's working with.

Components can [reference other components]({{< relref "component-identity.md" >}}), so a product made of multiple services
becomes a graph that travels as one unit.

## Sign

OCM signs the **component descriptor**, not individual files. Since the descriptor contains cryptographic digests of every artifact,
one signature covers everything.

The key property: **signatures are location-independent**. They stay valid no matter where the component ends up —
a different registry, a different continent, or the other side of an air gap.
Read more about [how signing works]({{< relref "signing-and-verification-concept.md" >}}).

This creates a **Software Bill of Delivery** (SBoD): a signed, verifiable record of exactly what was shipped.
For regulated industries that demand proof of what's running in production, this is essential.

## Transport

This is where OCM really shines.

Software needs to move between registries, across organizations, and into environments that may be completely disconnected.
Sovereign clouds add strict requirements: data residency, operational independence, and a fully auditable supply chain —
even without connectivity to the outside world.

OCM's [transfer model]({{< relref "transfer-concept.md" >}}) handles all of this.
The three basic patterns:

- **Registry → Registry** — direct copy between OCI registries.
- **Registry → CTF archive** — export to a portable file for air-gapped or offline transfer.
- **CTF archive → Registry** — import into the target environment.

At every step, signatures and metadata travel with the content. Nothing is lost, nothing needs to be re-signed.

## Deploy

Once your component arrives, [OCM controllers]({{< relref "ocm-controllers.md" >}}) take over on Kubernetes.
They pull new versions from the repository, adjust artifact references to the local environment (**localization**),
and reconcile the desired state in the cluster.

But deployment isn't a one-time event. The same Pack → Sign → Transport → Deploy cycle handles **day-2 operations**
— upgrades, config changes, migrations — entirely within the target's sovereign boundary. No callback to the source required.

{{< callout title="Secure Delivery for Sovereign Clouds" >}}
"Sovereign" means more than air-gapped. It means the target environment can receive, verify, deploy, and **upgrade** software completely on its own.
Components carry everything needed for the full lifecycle, making OCM a natural fit for regulated industries, government infrastructure,
and any scenario where supply-chain control is non-negotiable.
{{< /callout >}}

## Extensibility

OCM can't predict every registry, signing service, or deployment tool you'll use — so it doesn't try.
Instead, it offers a [plugin system]({{< relref "plugin-system.md" >}}) that lets you extend repositories, credentials,
signing algorithms, access methods, and more.

OCM adapts to your infrastructure. Not the other way around.

## Dive Deeper

- **[Getting Started]({{< relref "getting-started.md" >}})** — hands-on with the OCM CLI.
- **[Component Identity]({{< relref "component-identity.md" >}})** — the data model in detail.
- **[Signing]({{< relref "signing-and-verification-concept.md" >}})** — cryptographic integrity, explained.
- **[Transfer]({{< relref "transfer-concept.md" >}})** — how content moves across boundaries.
- **[Controllers]({{< relref "ocm-controllers.md" >}})** — automated deployment on Kubernetes.
- **[Plugins]({{< relref "plugin-system.md" >}})** — extending OCM for your stack.
- **[Benefits]({{< relref "benefits.md" >}})** — why OCM matters.
