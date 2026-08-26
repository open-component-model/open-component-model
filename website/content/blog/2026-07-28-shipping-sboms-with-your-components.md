---
title: "Shipping SBOMs with Your Components"
description: "A proof of concept for transporting Software Bills of Materials alongside OCM components: linking, discovering, and orchestrating SBOMs so a whole component version could be scanned in one command."
date: 2026-07-28T10:00:00+02:00
contributors: []
tags: ["supply-chain", "sbom", "security", "cli"]
draft: false
---

## The Problem

OCM already tells you *which artifacts* a component delivers: its Software Bill of Delivery. But that does not answer the question a security team actually has: *"which dependencies are bundled inside those artifacts, and am I affected by this vulnerability?"* Knowing a component ships an OCI image and a CLI binary as a resource does not tell anything about the packages and libraries buried inside that resource.

Those answers live in Software Bills of Materials (SBOMs), but today they are scattered: natively attached to an individual OCI artifact (so you have to know to look there), added to the component but with no relation to the resource they describe, or transported entirely outside the component, where they drift out of sync with what actually shipped and lose their trustworthiness. There is rarely one trustworthy answer for a whole component.

The idea explored here: make SBOMs travel *with* the component, related to the artifacts they describe, so the dependency-and-vulnerability question can be answered straight from the delivered component, online or air-gapped, and covered by the same trust as the rest of the signed component.

---

## What We Prototyped

To explore the idea, we built a proof of concept across three areas. None of this is a shipped feature yet: it is a spike to see what the workflow could feel like, and one we are now actively driving forward.

**Linking an SBOM to what it describes.** A resource of `type: sbom` carries an `ocm.software/artefactReference` label whose `identitySelector` points at the resource the SBOM is about. Because it is an ordinary, signed resource, the link travels and is verified with the component, with no side channel.

```yaml
- name: cli-sbom
  type: sbom
  labels:
    - name: ocm.software/artefactReference
      version: v1
      value:
        identitySelector:
          name: cli        # the resource this SBOM describes
      signing: true
  input:
    type: File/v1
    path: ./cli.sbom.json
    mediaType: application/vnd.cyclonedx+json
```

**Discovering SBOMs that already exist.** Many images already ship an SBOM (Docker buildx attestations, OCI Referrers). A prototype `SBoM/v1` input discovers that SBOM at build time and links it to the component (*discover, never generate*) so it works with artifacts as they are, in their original format.

```yaml
- name: podinfo-sbom
  type: sbom
  input:
    type: SBoM/v1
    resource:
      name: podinfo       # discover the SBOM attached to this image
```

**Consuming the result.** A single SBOM is just a normal resource download. The interesting part is the *orchestrating* SBOM: one command assembles every resource's SBOM into a single CycloneDX document (with `--recursive`, across referenced components too), which you can hand straight to a scanner:

```shell
ocm get sbom ghcr.io/org/product:v1 --recursive -o json > product.cdx.json
trivy sbom product.cdx.json
```

```text
┌────────┬──────────┬─────────────────┐
│ Target │   Type   │ Vulnerabilities │
├────────┼──────────┼─────────────────┤
│        │ gobinary │       58        │
└────────┴──────────┴─────────────────┘
```

The first command produces a SBoM for the whole component. The second answers "am I affected?" from the delivered component, online or air-gapped, with no per-artifact hunting.

We also explored embedding VEX (Vulnerability Exploitability eXchange) statements inside the SBOM itself and teaching [OpenDeliveryGear (ODG)](https://open-component-model.github.io/open-delivery-gear/) to consume them. A VEX statement carries the vendor's assessment of whether a vulnerability actually affects the product, so a scanner can suppress the ones already ruled out instead of flagging every match.

---

## What's Next

The proof of concept showed the workflow holds up end to end. We are now actively taking it forward, turning the prototype into a solid design. A few points are still to be clarified as we go:

- **Whether to track SBOMs as explicit resources.** A consumer has to be able to discover SBOMs directly anyway, so the feature works with components built *without* the core tooling, so we are weighing whether an explicit baked resource is needed at all, or whether to start with the smaller API surface and add it later.
- **How a discovered SBOM is referenced.** The prototype bakes a referrer SBOM as a local blob; referencing the original OCI artifact directly (image or layer) instead of duplicating it is the cleaner target we are working towards.
- **The label convention.** `ocm.software/artefactReference` follows an OCM spec convention still being finalized in [ocm-spec#146](https://github.com/open-component-model/ocm-spec/pull/146); the exact value shape may still change.

You can follow along, or chime in, on the epic and the pull request:

- Epic: [open-component-model/ocm-project#1225](https://github.com/open-component-model/ocm-project/issues/1225)
- Pull request: [open-component-model/open-component-model#3177](https://github.com/open-component-model/open-component-model/pull/3177)
