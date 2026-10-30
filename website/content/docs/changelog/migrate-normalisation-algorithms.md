---
title: "Migrate Normalisation Algorithms"
description: "Move signatures and component reference digests to the current default normalisation algorithm, gradually and with your regular releases."
icon: "arrow-autofit-right"
slug: "migrate-normalisation-algorithms"
weight: 60
toc: true
aliases:
  - /docs/how-to/migrate-normalisation-algorithms/
---

## Goal

Move the signatures and component reference digests of your component versions to the current default normalisation
algorithm, `jsonNormalisation/v4alpha3`.

{{< callout context="note" >}}
There is no urgency. Digests computed with older algorithms keep verifying, and so do component versions that mix
algorithms, for example a parent signed with `jsonNormalisation/v4alpha3` that references children built with an older
OCM CLI. Migrate gradually, as part of your regular releases. You do not need to switch everything at once.
{{< /callout >}}

## Why Migrate?

The normalisation algorithm decides which parts of a component descriptor a digest, and therefore a signature, covers.
Older algorithms may cover less of the descriptor than the current default (see
[Normalization and Digest Calculation]({{< relref "docs/concepts/signing-and-verification-concept.md#normalization-and-digest-calculation" >}})).
OCM logs a warning whenever it relies on such a digest and you can act on it:

- `trusted digest was computed with an older normalisation algorithm`: a signature checked by `ocm verify`, or a digest
  pin.
- `component reference digest was computed with an older normalisation algorithm`: a component reference in your
  component constructor (`ocm add cv`), or of a component version you transfer (`ocm transfer cv`).
- `signing with an older normalisation algorithm`: `ocm sign cv --normalisation` with an algorithm other than the
  default.

The OCM controller and `ocm add cv`, for the references of component versions published by others, log the same messages
at debug level only: these digests can only be upgraded by their publisher.

## Why Re-Signing Alone Is Not Enough

A signature covers the component version it is attached to, and reaches the component versions it references only
through their component reference digests. Each reference digest is part of the referencing descriptor and is computed
with its own normalisation algorithm, when the referencing component version is built with `ocm add cv`. `ocm sign`
never changes reference digests.

So a tree is only as strongly covered as the weakest algorithm along each reference. Re-signing a root with the default
upgrades its signature, but not the reference digests inside it, and not the references further down. To upgrade a tree,
build and sign its component versions with a current OCM CLI.

## Prerequisites

- A current [OCM CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}), on every machine that builds,
  signs, transfers or verifies your component versions
- Your component constructors

## Steps

### 1. Upgrade the consumers first

Component versions built with a current OCM CLI carry `jsonNormalisation/v4alpha3` digests. An older OCM CLI or OCM
controller does not know that algorithm: it fails a recursive `ocm transfer cv` or a reference digest check with
`unknown normalisation algorithm`. Upgrade the OCM CLI in your pipelines and the OCM controllers that consume your
component versions before you publish the first migrated version.

### 2. Find what still uses an older algorithm

Look at the `normalisationAlgorithm` of the signatures and component references of a component version and the
component versions it references:

```bash
ocm get cv <repository>//<component>:<version> --recursive -o yaml \
  | yq '.[] | {.component.name + ":" + .component.version: {"signatures": [.signatures[].digest.normalisationAlgorithm], "references": [.component.componentReferences[].digest.normalisationAlgorithm]}}'
```

Anything other than `jsonNormalisation/v4alpha3` is a candidate. The warnings listed above point to the same component
versions while you work with them.

### 3. Build the next release with a current OCM CLI

Build your next release of each component as usual, with a current OCM CLI. `ocm add cv` computes every component
reference digest that the constructor does not pin with the default. A `digest` you pin in the constructor is kept with
its algorithm, so remove pinned digests that use an older algorithm.

Build the referenced components before the components that reference them. Components you build in one
`ocm add cv` run are ordered for you. A referenced component version built and published by someone else keeps its own
references until its owner rebuilds it; your reference to it is upgraded either way.

{{< callout context="caution" >}}
Publish a new version instead of rebuilding an existing one. `ocm add cv` replaces an existing component version by
default. A rebuilt version has new reference digests and therefore a different digest, so existing pins and the
reference digests of component versions that reference it no longer match.
{{< /callout >}}

### 4. Sign with the default

Sign without `--normalisation`; the default is `jsonNormalisation/v4alpha3`:

```bash
ocm sign cv <repository>//<component>:<version> --signature <name>
```

### 5. Check the result

```bash
ocm verify cv <repository>//<component>:<version> --signature <name>
```

You should see `SIGNATURE VERIFICATION SUCCESSFUL` and no warning about an older normalisation algorithm. Older
references further down the tree are still reported by `ocm transfer cv --recursive` until their component versions are
rebuilt.

## Troubleshooting

### Symptom: `unknown normalisation algorithm: jsonNormalisation/v4alpha3`

**Cause:** The OCM CLI or OCM controller is older than the component version it reads.

**Fix:** Upgrade it (see step 1).

### Symptom: `reference digest uses a verify-only normalisation algorithm`

**Cause:** The constructor pins a component reference digest computed with `jsonNormalisation/v1`, which can only be
verified, never recorded in a new component version.

**Fix:** Remove the `digest` from that reference; `ocm add cv` computes one with the default.

## Related documentation

- [Concept: Signing and Verification]({{< relref "docs/concepts/signing-and-verification-concept.md" >}})
- [Reference: ocm sign component-version]({{< relref "docs/reference/ocm-cli/ocm_sign_component-version.md" >}})
