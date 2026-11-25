---
title: "Inspecting Every Attestation Attached to a Component"
description: "A new 'ocm get attestations' command merging OCI referrers, BuildKit in-toto, and OCM-local resources into one metadata-only supply-chain inspector."
date: 2026-11-25T10:00:00+01:00
contributors: []
tags: ["supply-chain", "security", "cli", "attestation"]
draft: false
---

## The Gap

An [earlier post]({{< relref "2026-07-28-shipping-sboms-with-your-components" >}}) described how SBOMs can travel
*inside* an OCM component, linked to the resources they describe, signed alongside them. That covered
the packaging side. What was still missing was the inspection side: a single command to answer the
question *"what security evidence is actually attached to this component right now?"*

The difficulty is that attestation tooling has converged on three separate attachment channels, all in
common use:

1. **OCI Referrers.** cosign, syft, vexctl, notary, oras and others push attestations, signatures and
   SBOMs as referrers of the artifact's digest, discoverable through the OCI Referrers API.
2. **BuildKit in-toto.** `docker buildx` embeds SBOM and provenance attestations directly inside the
   image index as `attests` manifest entries, tied to individual platforms.
3. **OCM-local resources.** The SBOM post introduced a resource-level label
   (`ocm.software/artifact-references`) that lets any resource inside the component declare itself as
   describing another resource: an SBOM, a VEX document, a SARIF file — anything.

A cosign signature, a syft SBOM, a docker buildx provenance and an OCM-linked VEX document can all
be attached to the same image. Before this work, no single query could surface all four at once.

---

## What We Built

`ocm get attestations` is a supply-chain inspector. Point it at a component version and it queries
every attachment channel, merges the results, and lists what it finds.

```shell
ocm get attestations ghcr.io/acme//acme.org/app:1.0.0
```

By default it shows the referrers attached to the **component version artifact** itself. Widen or
narrow from there:

```shell
# Every resource of the component, plus the component's own referrers
ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 -o widetree

# Only the resource named "image"
ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 --identity name=image

# Follow componentReferences recursively — list a whole product, not just one component
ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 --recursive -o widetree

# Only SBOMs and VEX documents, as machine-readable JSON
ocm get attestations ghcr.io/acme//acme.org/app:1.0.0 --type sbom --type vex -o json
```

### Three sources, one answer

Per resource the command merges attestations from all three channels and deduplicates them:

- OCM-local resources whose `ocm.software/artifact-references` label points at this resource —
  these travel in the component descriptor and work in air-gapped or transferred environments.
- OCI referrers on the resource's artifact — discovered by walking the OCI Referrers API, covering
  anything pushed by cosign, syft, vexctl, notary or oras.
- BuildKit in-toto attestations embedded in the artifact's image index — per-platform, without
  fetching the manifest separately.

For multi-component products, `--recursive` follows `componentReferences` to any depth (`-1` for
unlimited) so one invocation covers the whole delivery graph.

### Deliberately unopinionated

The command is **metadata-only**: it reads manifests and annotations but never fetches document
blobs and never verifies a signature. Everything it needs to identify and classify an attestation
is in the metadata layer.

It is also **deliberately non-classifying**. The `TYPE` column shows the raw type signal exactly as
the tool that attached it wrote it: the in-toto predicate type (e.g.
`https://spdx.dev/Document`), the OCI artifact type (e.g. `application/vnd.sigstore.bundle+json`),
or the document media type (e.g. `application/vnd.cyclonedx+json`). There is no fixed vocabulary
to map into, and no attestation is hidden because its type string was not recognised.

The `--type` flag handles the cases where you *do* want to narrow down: it matches a substring of
any raw type signal, plus a set of friendly aliases so you can write `sbom` and have it match both
CycloneDX and SPDX documents, or `signature` and catch both cosign bundles and Notation signatures.

### Output formats

Five output formats serve different consumers:

| Format        | Use case                                                                                      |
|---------------|-----------------------------------------------------------------------------------------------|
| `table`       | Default; each attestation is a flat row                                                       |
| `tree`        | Provenance tree with full digests, grouped by source — mirrors `oras discover`'s output shape |
| `widetree`    | Same tree, but every resource of the component is a node                                      |
| `json`/`yaml` | Machine-readable; carries raw type signals, creation timestamps, and the full annotation map  |

The tree formats surface containment explicitly: *reference → origin (resource or component) → source (referrer / index / local) → platform → attestation*, each leaf carrying the full digest.

![ocm get attestations -o widetree --recursive: the full supply-chain tree for a two-component product, showing OCI referrers, BuildKit in-toto index attestations, and an OCM-local resource across two platform variants](/images/get-attestations-widetree.png)

Resources that have no attestations in the registry (a local blob that is not an OCI manifest, for
example) are skipped before resolving, so the command does not make a slow empty referrer probe for
every resource.

---

## What's Next

This is an EXPERIMENTAL command. The discovery model is in place and covers all three attachment
channels, but a few things are still ahead:

- **Downloading and verifying.** Right now the command is an inventory tool. The natural next step
  is `ocm get attestation --download` for a single document and an `--verify` path that checks the
  signature covering an attestation, following the same trust chain as `ocm verify`.
- **The label convention.** `ocm.software/artifact-references` follows a spec convention being
  finalized in [ocm-spec#146](https://github.com/open-component-model/ocm-spec/pull/146); the exact
  value shape may still change, and the command will track it.
- **Integration with OpenDeliveryGear.** The earlier SBOM post explored feeding the output to ODG
  for VEX-aware scanning. That path opens up once the download step is in place.

The pull request is open for feedback:

- Pull request: [open-component-model/open-component-model#4391](https://github.com/open-component-model/open-component-model/pull/4391)
