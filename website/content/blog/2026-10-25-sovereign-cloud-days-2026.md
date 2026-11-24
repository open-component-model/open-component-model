---
title: "OCM @ Sovereign Cloud Days 2026: Sovereign Cloud and Air-Gapped Delivery"
description: "OCM on the Sovereign Cloud Days 2026 Main Stage: shipping software of any size into sovereign and air-gapped environments."
date: 2026-10-25T19:00:00+01:00
contributors: []
tags: ["ocm", "sovereignty", "air-gap", "signing", "transfer", "neonephos", "conference"]
draft: false
---

## Sovereign Cloud Days 2026 in Saarbrücken

From October 22 to 25, 2026, the [Sovereign Cloud Days](https://www.sovereign-cloud-days.eu/en) brought decision-makers,
operators, and the open source community together in Saarbrücken under one motto: _Digital Sovereignty. Open. Practical. Now._
The weekend belonged to the community-organized **OpenInfra & Cloud Native Track** on the campus of
[htw saar](https://www.htwsaar.de/), run in cooperation with the OpenInfra Foundation and the CNCF. No vendor pitches,
just real experiences from production operations.

![The Main Stage at htw saar during Sovereign Cloud Days 2026](/images/sovereign-cloud-days-2026-main-stage.jpg)

On Sunday, October 25, Jakob Möller took the Main Stage to present
**Sovereign Cloud and Air-Gapped Delivery with Open Component Model**. The question behind the talk is simple: what does it
take to deliver software into an environment you are not allowed to call home from?

![Jakob Möller opening the OCM talk on the Main Stage](/images/sovereign-cloud-days-2026-ocm-talk.jpg)

Page through the full slide deck here, or read on for the highlights:

{{< slides dir="images/sovereign-cloud-days-2026-slides" title="Sovereign Cloud and Air-Gapped Delivery" pdf="/slides/ocm-sovereign-cloud-days-2026.pdf" >}}

## Sovereign readiness is an operating model, not a checkbox

Sovereign cloud is not a deployment target you add at the end of a release. It changes how a product must be built and
shipped. The talk framed sovereign readiness around three principles:

1. **Configure, don't rebuild.** Regional differences belong in a supported configuration contract, not in a fork of the
   product.
2. **Operate independently.** Deployment, validation, troubleshooting, and recovery must be executable by the operator
   inside the sovereign environment.
3. **Stay inside the boundary.** Artifacts, tooling, telemetry, licensing, and dependencies resolve in-region or turn off.

These principles boil down to one operating test:

> Can an operator deploy, upgrade, validate, and recover the product using only shipped artifacts, automation,
> configuration, and release-specific documentation?

![Jakob explaining what it means to stay inside the sovereign boundary](/images/sovereign-cloud-days-2026-sovereign-readiness.jpg)

The hard part is that real products are rarely a single artifact. Running one application means delivering the service
stack _and_ everything it references: container images, Helm charts, Kubernetes manifests, infrastructure dependencies such
as databases, Day-2 operators, and foundational pieces down to bare-metal provisioning. Each comes from its own source, in its
own format, through its own pipeline, deployed by Terraform, Crossplane, Argo CD, Flux, Ansible, and friends. Moving that
landscape into a sovereign or air-gapped region by hand does not scale, and it does not survive an audit.

## One identity, every boundary

This is the problem OCM was built for. OCM describes _every_ artifact type (OCI images, Helm charts, npm packages, binaries,
configuration, or anything else) under one component identity, and lets you move it across _every_ deployment boundary:
EU, US, sovereign cloud, or fully disconnected environments. The same evidence then serves compliance frameworks such as
DORA, NIS2, and the Cyber Resilience Act.

The key idea is that **identity and trust travel with the release**:

- **Component identity**: a globally unique, location-agnostic name and version such as `github.com/acme.org/helloworld:1.0.0`.
- **Digest**: every resource in the component carries a content hash, computed once.
- **Access**: where a resource currently lives. On transfer, only the access is rewritten. The digest stays.

A component version starts from a constructor file you write:

```yaml
components:
  - name: github.com/acme.org/helloworld
    version: 1.0.0
    provider:
      name: acme.org
    resources:
      - name: mylocalfile
        type: blob
        input: # embed by value
          type: File/v1
          path: ./my-local-resource.txt
      - name: image
        type: ociImage
        version: 1.0.0
        access: # reference an external artifact
          type: OCIImage/v1
          imageReference: ghcr.io/stefanprodan/podinfo:6.9.1
```

The resulting component descriptor carries the digests of all resources. A signature covers one hash over the normalized
descriptor, which makes it a Merkle proof over everything you ship. Because the access information is excluded from the
signed digest, you can relocate the component from a public registry to `registry.local` inside an air gap and still verify
the exact same signature there. Read more in [Transfer and Transport](/docs/concepts/transfer-and-transport/)
and [Signing and Verification](/docs/concepts/signing-and-verification/).

![Jakob walking through the transport of a signed component into an air-gapped environment](/images/sovereign-cloud-days-2026-transport.jpg)

## Pack · Sign · Transport · Deploy

The talk condensed the OCM workflow into four steps:

1. **Pack**: bundle your software into components. One source of truth.
2. **Sign**: one signature covers all artifacts.
3. **Transport**: across any boundary, even air-gapped.
4. **Deploy**: the OCM Kubernetes controllers verify, unpack, and deploy at the destination. No callback upstream.

Same identity, same signature, any location. Operators and auditors inside the sovereign environment verify locally and
run Day-2 operations with what was shipped. Nothing has to reach back to the source.

If you want to try this yourself, follow
[Transfer Components across an Air Gap](https://ocm.software/main/docs/guides/transfer/transfer-components-across-an-air-gap/) and
[Verify Component Signatures on Deployment](https://ocm.software/main/docs/guides/deploy/verify-component-signatures-on-deployment/).

## From a single service to a full sovereign product

Sovereign delivery is not only a problem for hyperscale platforms. A small team shipping one service into a customer's
private data center faces the same boundary as an enterprise moving hundreds of services into a sovereign region. OCM is
designed to scale across that whole range with the same model:

- **Service components** carry their own resources: images, charts, configuration, SBOMs, and more.
- **Product components** compose other components through references. A product becomes one release unit that is
  transferable and signable end-to-end.

```yaml
components:
  - name: acme.org/sovereign/product
    version: 1.0.0
    provider:
      name: acme.org
    componentReferences: # no resources of its own, pure composition
      - name: notes
        componentName: acme.org/sovereign/notes
        version: 1.0.0
      - name: postgres
        componentName: acme.org/sovereign/postgres
        version: 1.0.0
```

Start with a single component for one service. When your landscape grows, compose those components into products, and
compose products into platforms, without changing tools or formats along the way. See
[Add Component References](https://ocm.software/main/docs/guides/pack/add-component-references/) for a hands-on walkthrough.

This range is not theoretical. OCM is open source by design, part of the [NeoNephos Foundation](https://neonephos.org/)
under the Linux Foundation Europe, and in use from small-scale setups to enterprise landscapes, including
[Gardener](https://gardener.cloud/), [Kyma](https://kyma-project.io/), [OpenControlPlane](https://github.com/openmcp-project),
and [Konfidence](https://konfidence.cloud/).

## Explore OCM yourself

The talk closed with a simple adoption path:

- **Pilot**: pack one product as an OCM component.
- **Standardize**: make OCM the default way you deliver components.
- **Steward**: bring yourself into the OCM community.

The [Getting Started]({{< relref "docs/getting-started/_index.md" >}}) guides are the fastest way to the first step, and
the [community page]({{< relref "community/_index.md" >}}) shows how to reach us.

A big thank you to the organizers, htw saar, and everyone who joined the talk and the hallway conversations afterwards.
Sovereignty only works if it is open and practical, and that is exactly what this weekend was about. See you next year!

_Photos: Julia Schygulla for Sovereign Cloud Days 2026._
