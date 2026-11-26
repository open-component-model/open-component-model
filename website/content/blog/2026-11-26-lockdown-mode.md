---
title: "Lockdown Mode: Localize at Import, Run Without OCM"
description: "A proposal for sealing a component version to its target environment: localized and signed at import, deployed with plain Flux."
date: 2026-11-26T10:00:00+01:00
contributors: []
tags: ["localization", "signing", "sovereign-cloud", "transfer", "supply-chain"]
draft: true
---

{{< callout context="note" title="This is a proposal" >}}
Lockdown mode is a design under discussion, not a released feature. The full design and
the discussion are in
[the design pull request](https://github.com/open-component-model/open-component-model/pull/4415).
{{< /callout >}}

## The problem

After a transfer, images live in a new registry, but Helm charts and manifests still
point at the old one. Today we fix that **at runtime, in the cluster**: OCM controllers
resolve the new locations and kro renders them into Flux objects.

That works, but it means every target cluster needs the OCM controllers and kro just
to deploy. In sovereign and air-gapped environments this is the hardest part to sell:
more components to approve, operate and trust.

Fixing the references **during transfer** was considered before and set aside, because
changing a chart changes its digest and breaks the producer's signature.

## The idea

Accept that the signature changes — and make it the feature.

`ocm transfer cv --lockdown` is the last hop into an environment. It verifies what the
producer shipped, rewrites the deployables for this environment, and seals the result
with the importer's key. The sealed component is bound to this environment and cannot
be transferred further.

```shell
ocm transfer cv --lockdown ctf::./airgap//acme.org/product:1.0.0 registry.internal/ocm
```

## What it does

1. **Verify.** Producer signature, reference digests, and every resource's bytes
   against the signed digests.
2. **Localize.** Image references in charts and manifests are rewritten to the new
   locations. Automatic, by convention: no rules to write. Each edit is proven by
   rendering the chart again, and the import fails if any image would still be pulled
   from outside. This happens before anything is written.
3. **Copy.** Everything lands as an OCI artifact in the target registry, pinned by
   digest. Charts and manifests arrive in their localized form only.
4. **Sign every artifact** with the importer's key (cosign).
5. **Lock down.** Sign each component version's stored descriptor, accesses included.
   Parents reference children by the digest of the stored artifact, so one signature
   on the root covers the whole tree and every location in it.

## What the cluster needs afterwards

|                       | Today                                 | Lockdown mode                                  |
|-----------------------|---------------------------------------|------------------------------------------------|
| In the cluster        | OCM controllers, kro, Flux            | Flux                                           |
| User writes           | OCM objects + ResourceGraphDefinition | Flux `OCIRepository` / `HelmRelease`           |
| Localization happens  | At runtime, per cluster               | Once, at import                                |
| Failures show up      | At deploy time                        | At import, before anything runs                |
| Trust root in cluster | Producer key, via OCM controller      | Importer key, via Flux and an admission policy |

The chain is plain cloud-native tooling: Flux verifies the chart signature, the chart
pins images, an admission policy (Kyverno) requires the importer's signature on every
image. No OCM component runs in the cluster.

## What it adds for security

- **Accesses are sealed.** Today a signature does not cover where an artifact is
  stored; anyone with registry write access can repoint it. In lockdown mode that
  breaks the signature.
- **One key, one policy.** Everything in the environment's registry is signed by the
  importer. The cluster rule is one line: accept only that key.
- **Verification is cheap.** One signature check plus digest comparisons covers the
  whole component tree.

## The trade-offs

- **The producer's signature does not survive.** It is verified at import and then
  replaced by the importer's. Inside the environment, the importer is the trust root.
  The unmodified, producer-signed transport archive remains the proof of origin.
- **A locked component version cannot leave.** Same name and version as the source,
  different content. It is an environment-bound derivative; transfer refuses it.
- **Localization is convention-based.** It covers the common chart and manifest
  patterns and fails loudly on the rest. Explicit override rules exist for those.
- **You write the Flux objects yourself.** Today a component can ship its own install
  recipe: which charts to install, in what order, with which values. That recipe needs
  kro in the cluster, so in lockdown mode you write an `OCIRepository` and a
  `HelmRelease` per chart, as for any other Helm chart.

## What it asks for

**From component authors**

- A chart and the images it uses belong to the same component version.
- `ocm add cv` records each image's original location in a signed label, so the
  importer knows what to look for. Automatic for OCI images; one line in the constructor
  for images built from local input. Components built before this need a rebuild.

**From the environment**

- A registry with immutable tags and the OCI 1.1 Referrers API.
- One importer key per environment.
- Flux, and Kyverno for the image policy.

## Positioning

Lockdown mode does not replace the controllers. It is a second way to consume OCM:

- **Controllers + kro**: continuous, self-contained delivery, producer signature
  verified in the cluster.
- **Lockdown**: OCM stops at the registry. Maximum simplicity and standard tooling at
  runtime, in exchange for an explicit import step.
