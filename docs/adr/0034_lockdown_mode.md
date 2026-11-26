# Design: Lockdown Mode — Localization and Sealing at Import

* **Status**: draft
* **Deciders**: OCM Maintainer Team
* **Date**: 2026-11-26

Technical Story: Localization happens at runtime today and needs the OCM controllers and
kro in every target cluster. This document designs an alternative for the last hop into
an environment: `ocm transfer cv --lockdown` verifies a component version, localizes its
deployables, signs every artifact, and seals the result to that environment, so that the
cluster needs no OCM tooling at all.

## Table of Contents

* [1. Context and Problem Statement](#1-context-and-problem-statement)
* [2. Goals](#2-goals)
* [3. Overview](#3-overview)
* [4. Design](#4-design)
  * [4.1 Lockdown signature](#41-lockdown-signature)
  * [4.2 Artifact signatures](#42-artifact-signatures)
  * [4.3 Localization at import](#43-localization-at-import)
  * [4.4 Producer signatures](#44-producer-signatures)
  * [4.5 Deep verification at import](#45-deep-verification-at-import)
  * [4.6 Runtime model](#46-runtime-model)
  * [4.7 Identity and create-once](#47-identity-and-create-once)
  * [4.8 Requirements on the environment](#48-requirements-on-the-environment)
  * [4.9 CLI surface and inputs](#49-cli-surface-and-inputs)
* [5. Decisions](#5-decisions)
* [6. Related Work](#6-related-work)
* [7. Open Questions](#7-open-questions)
* [8. Plan](#8-plan)
* [9. Risks](#9-risks)

## 1. Context and Problem Statement

After a transfer, images live in a new registry, but Helm charts and manifests still
point at the old one. Adjusting them is *localization*.

* **Today** it happens at runtime: Repository → Component → Resource → Deployer objects,
  and a kro `ResourceGraphDefinition` renders Flux objects from Resource status
  ([ADR 0015](0015_sovereign_cloud_reference_scenario.md)). Every target cluster needs
  the OCM controllers and kro to deploy.
* **Transfer-time localization** was explored in
  [ADR 0004](0004_localization_at_transfer_time.md) (content mutation, undecided) and
  [ADR 0026](0026_localization_at_transfer.md) (wrapper charts, rejected). The recurring
  objection: changing a chart changes its digest and breaks the producer signature.

Producer signatures are location-independent by design. The descriptor normalisation
(`descriptor/normalisation/json/v4alpha1`) excludes `access`, `repositoryContexts`,
`srcRefs`, non-signing labels and `signatures`. Two consequences matter here:

* A signature says nothing about where an artifact is stored. Anyone with write access
  to the registry can repoint an access.
* `ocm verify` checks the root descriptor only, and resource download verifies bytes
  against the access digest, not the signed resource digest.

Lockdown mode accepts the digest change and turns it into the feature: the import is an
explicit, terminal step with its own signature.

## 2. Goals

* **G1 — OCM-free runtime.** Deploy in sovereign and air-gapped environments with plain
  Flux, Helm and kubelet. No OCM controllers, no kro.
* **G2 — Hardening.** Accesses cannot be edited after import without detection.

These are two separable features:

| Goal | Delivered by |
|------|--------------|
| G1 OCM-free runtime | import-time localization, artifact signatures, digest pinning |
| G2 access hardening | lockdown signature on the stored component version |

Without OCM controllers nothing in the cluster reads the descriptor. The lockdown
signature protects tooling, audit and CI gates. Runtime protection comes from artifact
signatures and digest pinning.

Non-goals: replacing the controllers, rollback or freshness protection, generating
deployment wiring.

## 3. Overview

*Import* is the informal name of the operation; *locked* describes the result.

1. **Verify** the producer signature, reference digests, and resource bytes.
2. **Copy** every resource into the target registry as an OCI artifact, pinned by digest.
3. **Localize** charts and manifests, by convention.
4. **Sign every artifact** with the importer key (cosign).
5. **Lock down**: sign each component version's stored OCI descriptor. Parents reference
   children by that digest, so the root signature covers the closure.

Lockdown happens only at the final hop. Air gap: plain `ocm transfer` into a CTF
(portable, producer-signed) → carry → `ocm transfer cv --lockdown` into the target
registry.

## 4. Design

### 4.1 Lockdown signature

A **detached cosign signature on the component version's top-level OCI descriptor**
(manifest, or index when OCI artifacts are stored as local blobs), by digest, made with
the same signer as the artifact signatures.

* No new normalisation algorithm. The registry content-addresses the exact bytes, and a
  detached signature does not have to exclude itself. It covers the plain descriptor —
  accesses, repository contexts, all labels, remaining `signatures` — and the local blob
  layers the manifest lists.
* Preconditions before signing:
  * every access is digest-pinned (`repo:tag@sha256:…`; `localBlob` already is);
  * signatures invalidated by the import are removed (§4.4).
* Graph order: upload artifacts → final accesses known → build descriptor → upload
  descriptor → sign its digest.

**Closure binding.** Import writes the child's top-level OCI descriptor digest into the
parent's `references[].digest`, instead of the normalised descriptor digest. Candidate
algorithm name: `ociArtifactDigest/v1`, already used for resource digests with the same
meaning.

* The tree becomes a Merkle tree over stored bytes: root signature → root manifest →
  child manifest digests → their accesses, recursively. One signature verifies the whole
  closure including every location.
* Children can be resolved by digest (`component-descriptors/<name>@sha256:…`), so a
  moved tag cannot swap a child. This needs a by-digest lookup in the repository API;
  until then verification resolves the tag and compares digests.
* The upload order becomes bottom-up, and the upload node must output the manifest
  digest. Today references pass through verbatim.
* Existing tooling is expected to fail closed on such a reference, because
  `verifyReferenceDigests` runs the named normalisation over the child descriptor.

Every component version is still signed individually: everything in the registry is
signed, and any component version can be a verification entry point.

**Verification.** `ocm verify cv --lockdown` resolves tag → digest, verifies the detached
signature, checks that accesses are pinned and walks the reference digests. An optional
deep mode re-digests blobs and checks that artifact signatures are present. The existing
`signing.Handler` flow over descriptor `signatures` is not used.

**Transfer.** `ocm transfer` refuses a locked component version.

What this buys:

* A shallow verification is sound: verified lockdown signature plus pinned accesses
  gives content integrity without re-digesting every blob.
* Access edits are detected.

Costs:

* OCI-only. Acceptable, because lockdown happens at the final hop, which is a registry.
* The signature is not visible in the descriptor; it is discovered via referrers.
* Any later descriptor write changes the manifest digest and breaks the lockdown. This
  is intended.

### 4.2 Artifact signatures

* A graph node `SignOCIArtifact` runs after each push and signs by digest.
* It covers images, Helm OCI charts, manifest artifacts and the component version
  manifest itself.
* Everything must land as an OCI artifact in the target registry. Plain manifest blobs
  are packaged as Flux OCI artifacts; Helm charts go through `ConvertHelmToOCI`.
* **Every artifact is signed with the importer identity**, including artifacts that
  already carry a signature. Whether an existing signature is verifiable depends on the
  runtime's trust policy, which import does not know. One importer key gives one cluster
  policy. Existing signatures are copied alongside.
* cosign is used with a key or KMS reference. Signatures are stored as OCI 1.1 referrers
  in the Sigstore bundle format, the cosign v3 default.
* A signed chart that pins image digests already gives image integrity through kubelet.
  Runtime values can reintroduce foreign images, though, and Flux only verifies sources.
  An admission policy on the importer key is the enforcement backstop.

### 4.3 Localization at import

Graph nodes `LocalizeHelmChart` and `LocalizeManifests` sit between download and OCI
conversion. Within one component version the order is images → charts → manifests.

Localization is **convention-based and automatic**. It is safe because import does not
guess what an image is.

#### Ground truth

For every transferred image import knows the pair *original location* → target
reference and digest. Every edit replaces a known original location, or its registry,
repository or tag parts, with its target. Nothing else is touched.

The original location is the reference the deployables were written against. It is
**not** the current source access, and it does not survive a hop:

* `ocm transfer --copy-resources` into a CTF turns
  `ociArtifact{imageReference: ghcr.io/acme/app:1.0.0}` into
  `localBlob{referenceName: acme/app:1.0.0}`. `getReferenceName` in
  `transfer/internal/oci.go` strips the registry host.
* A resource built by value (`input: file`, OCI layout) never had an image reference in
  any access.

#### Original-locations label

A signed resource label records the location(s) under which deployables know the
artifact:

```yaml
resources:
  - name: app-image
    type: ociImage
    version: 1.0.0
    labels:
      - name: ocm.software/original-locations
        version: v1alpha1
        signing: true
        value:
          - imageReference: ghcr.io/acme/app:1.0.0
    access:
      type: ociArtifact
      imageReference: ghcr.io/acme/app:1.0.0
```

* Placed on the referenced artifact (the image), not on the chart.
* A list, because one image has several spellings (`postgres:18`,
  `docker.io/library/postgres:18`). Objects rather than strings leave room for other
  location kinds later.
* `imageReference` is stored as written. The matcher parses it and normalises familiar
  names.
* **Always a signing label.** The producer signature covers the mapping.
* Written at construction only: `ocm add cv` adds it for OCI image accesses when absent;
  authors set it by hand for by-value inputs. An existing label is never overwritten.
* **Transfer never writes it.** A signing label added after signing would break the
  producer signature, and a non-signing one is not trusted.
* Import uses the label only when it is a signing label on a component version whose
  producer signature verified. A tampered mapping cannot redirect a chart.
* Components built before the label exists need a rebuild or explicit override rules.

[OCM-native deployment wrappers ADR, PR #4101](https://github.com/open-component-model/open-component-model/pull/4101)
proposes a label with the same purpose, `ocm.software/localization`. The two must
converge on one label, see [§7](#7-open-questions).

#### Package rule

**A deployable's images must be resources of the same component version.** A chart and
its images form a package.

* The candidate set for a chart is the images of its own component version.
* Localization dependencies never cross component versions.
* An umbrella chart whose subchart images live in another component fails, by design.
* Optional narrowing: `ocm.software/artifact-references` on the chart, listing the images
  it uses. When present, matching is restricted to those.
* The sovereign scenario already complies: `notes` and `postgres` each carry their image
  and chart.

#### Detection

1. Helm values conventions: `{registry, repository, tag, digest}`, host-embedded
   `repository`, scalar `image: <ref>`, lists, global registry keys, subchart value
   scopes.
2. Plain manifests: pod-spec field paths for built-in kinds plus configurable CRD paths,
   as in the kustomize image transformer.
3. Literal fallback: exact original-location strings anywhere in the files, including
   chart templates and env or args values.

#### Safeguards

* **Generate and test.** An edit is accepted only if rendering the chart before and
  after proves that the image references moved to the targets. Helm v4 is already a
  direct dependency.
* **Fail closed.** After localization, any rendered image reference that does not map to
  a resource of the same component version fails the import.
* **Report.** Import prints every edit (file, path, old → new).
* **Explicit rules** remain as an override in the lockdown configuration, for what
  conventions cannot reach.

#### Limits

* **Digest pinning is best effort.** Charts that render `repo:tag` ignore a `digest`
  value. Where no convention pins, the tag reference stays. Target tags are immutable
  (§4.7) and the image signature policy is the backstop.
* **Determinism depends on the localizer version.** A newer convention set can produce
  different bytes for the same input, which then conflicts with the immutable tag.
* **Rendering covers default values.** Images behind disabled feature flags are rewritten
  when they match, but reported as unverified. A chart's `ci/*-values.yaml` can serve as
  extra render inputs.
* Images composed from fragments in templates and non-YAML configuration are out of
  reach for conventions.

### 4.4 Producer signatures

* Signatures are verified at import. Those the import invalidates are removed from
  `signatures`; those still valid stay.
* **Invalidation propagates up the reference tree.** With manifest digests as reference
  digests, every reference digest is rewritten, so every non-leaf component version
  loses its producer signature, even when nothing was localized. Leaves without
  localized resources keep theirs.
* Removed signatures are dropped, not preserved. A provenance record inside the locked
  component version was considered and deferred.
* Provenance therefore lives outside the target registry: the producer-signed,
  unmodified transport archive. With the same localizer version an auditor can re-derive
  the locked component version from it and compare manifest digests.
* In the target environment the importer key is the trust root. This matches ADR 0004:
  the landscape operator is the final authority.

### 4.5 Deep verification at import

Before anything is signed, import verifies:

* the producer signature on the root;
* reference digests through the closure;
* resource bytes against the **signed** resource digests. This is a gap today;
  [ADR 0033](0033_resource_digest_rules.md) provides the digest rules.

### 4.6 Runtime model

The user authors the Flux objects (`OCIRepository`, `HelmRelease`, `Kustomization`), in
place of the OCM objects authored today. Import neither generates nor localizes
deployment wiring.

* Localization covers the content of charts and manifests only.
* The locked descriptor is the source of truth for locations. Import prints a location
  report: resource identity → `ref@digest`, plus the key reference for `spec.verify`.
* Upgrade: import the new version, then bump the reference in the Flux objects.
* What a `ResourceGraphDefinition` encoded (ordering, value wiring between charts)
  becomes the user's Flux configuration.
* Trust chain in the cluster: Flux `OCIRepository.spec.verify` (importer key) →
  localized chart → pinned image digests → kubelet, with a Kyverno
  `ImageValidatingPolicy` on the importer key as backstop.

### 4.7 Identity and create-once

ocm-spec gives a component version a globally unique identity. What must not vary is the
normalised content; accesses, repository contexts, non-signing labels and signatures may
differ per repository. With a plain transfer every copy has the same normalised digest
and satisfies every parent.

Lockdown breaks that equivalence twice: localization changes resource digests, and
manifest digests as reference digests bind parents to exact bytes.

* **Globally**, source and target hold different content under one identity. This is
  accepted and scoped: a locked component version is an environment-bound derivative
  that cannot leave.
* **Inside one target repository** a locked `name:version` is create-once.
  * This is enforced by the registry through **immutable tags**. A check in the tooling
    would be a time-of-check to time-of-use race.
  * Import still resolves the tag first, for usability: identical digest → skip the
    push; different digest → fail early. A rejected push counts as a conflict.
  * By-digest references give integrity, since a moved tag is detected. Immutable tags
    give availability, since nothing a parent references gets untagged and
    garbage-collected.
  * One target repository has one import configuration. A dedicated repository for
    locked content is recommended.

### 4.8 Requirements on the environment

* **Immutable tags** in the target registry (§4.7).
* **OCI 1.1 Referrers API.** A referrer-stored signature is a manifest with a `subject`,
  pushed by digest, so no tag is written and adding a signature is just another
  manifest. Two tag-writing paths must be avoided: cosign's legacy `sha256-<digest>.sig`
  scheme, and the referrers tag-schema fallback used when the registry lacks the API.
  OCM's own ownership referrers and component index have the same exposure.
* The target must be addressed **as the cluster sees it**. An explicit runtime base URL
  covers setups where importer and cluster use different host names.
* **One importer key per environment.** A repository-context check was considered and
  deferred; separate keys prevent a locked component version from one environment being
  accepted in another.
* Flux with support for cosign v3 bundles, and Kyverno for the image policy. sigstore
  policy-controller cannot verify key-signed bundles today.

### 4.9 CLI surface and inputs

`ocm transfer cv --lockdown`, with `ocm verify cv --lockdown` as its counterpart.

The flag is a switch. Inputs follow two existing patterns: `ocm sign cv` takes the signer
from the OCM configuration and key material from the credential graph, and transfer
settings are typed configuration entries
([ADR 0018](0018_transfer_configuration.md)).

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: lockdown.transfer.config.ocm.software/v1alpha1
    signer:                         # cosign key / KMS reference, never key material
      type: CosignSigner/v1alpha1
    verify:                         # producer signatures that must verify at import
      signatures: [acme]
    runtimeBaseURL: registry:5000   # optional, registry address as the cluster sees it
    localization:                   # optional explicit overrides
      rules: []
```

Type and field names are placeholders. The importer key and the producer public key come
through `credentials.config.ocm.software`.

* Flag and configuration entry are both required. A configuration file alone must never
  turn a transfer into a sealing operation; the flag alone is an error.
* Missing signer or verification credentials are a hard error before anything is
  written.
* `--lockdown` implies full recursion and `unmatchedResourcePolicy: Fail`, and requires
  every resource to land as an OCI artifact. Contradicting input (non-recursive,
  by-reference resources, CTF target, `--transfer-spec`) is rejected.
* `--dry-run` builds and validates the graph and signs nothing.

A dedicated `ocm import cv` would duplicate the transfer flag surface for the same graph
builder. The controllers consume the transfer configuration, not CLI verbs.

## 5. Decisions

| # | Topic | Decision | Alternative considered |
|---|-------|----------|------------------------|
| D1 | Localized content | Mutate in place. The descriptor stays self-consistent; `ocm download resource` yields the deployable. | Keep originals and add derived artifacts. Producer signature stays valid, but two charts per resource and a modelling workaround. |
| D2 | Signer | cosign with key or KMS, signatures as OCI 1.1 referrers. Already bundled. | notation X.509, a new dependency. |
| D3 | Localization source | Conventions, automatic, with generate-and-test and a fail-closed gate. Explicit rules as override. | Explicit rules as the default path. |
| D4 | Repository-context binding | Deferred. One registry has several valid addresses; one importer key per environment instead. | Record and enforce the target repository at verify. |
| D5 | Which artifacts are signed | All, with the importer identity; existing signatures copied. | Only artifacts without a runtime-verifiable signature. |
| D6 | Deployment wiring | Authored by the user as Flux objects. | Component ships static Flux manifests that import localizes. |
| D7 | CLI surface | `ocm transfer cv --lockdown` over a typed configuration entry. | Dedicated `ocm import cv`. |
| D8 | Lockdown signature | Detached signature on the stored OCI descriptor. | In-descriptor signature with a new normalisation; works for non-OCI repositories but needs a spec change. |
| D9 | Importer signature in the descriptor | None. | Additional standard-normalisation signature on the root for unmodified tools. |
| D10 | Closure binding | Child OCI descriptor digest as reference digest. | Normalised reference digests plus manifest digests in a non-signing label. |
| D11 | Same identity, different content | Accepted, scoped to the environment; locked component versions cannot leave. | Version suffix at import, or a pure overlay that leaves producer-signed content untouched. |
| D12 | Original image locations | Signed resource label, written by `ocm add cv`. | No label; works only for a single hop from the original registry. |
| D13 | Where a deployable's images live | Same component version as the deployable. | Anywhere in the closure. |

## 6. Related Work

* [ADR 0004](0004_localization_at_transfer_time.md): transfer-time localization by
  content mutation. Lockdown follows its trust model.
* [ADR 0005](0005_transformation.md): transformation specification; the graph that
  lockdown extends.
* [ADR 0026](0026_localization_at_transfer.md): wrapper charts and convention scanning.
* [OCM-native deployment wrappers ADR, PR #4101](https://github.com/open-component-model/open-component-model/pull/4101):
  convention-based localization at deploy time from a signed label, without kro. It
  solves the same matching problem at a different time; label and matcher should be
  shared.
* [Normalized OCI layout, PR #3888](https://github.com/open-component-model/open-component-model/pull/3888):
  cosign-signable component versions whose signature survives transfer. The opposite
  property of a lockdown signature; the layouts must be reconciled.
* [Notation signing handler, PR #3625](https://github.com/open-component-model/open-component-model/pull/3625)
  and [remote signing, PR #3909](https://github.com/open-component-model/open-component-model/pull/3909):
  relevant for the importer key.

## 7. Open Questions

* **Label convergence.** The deployment wrappers ADR (PR #4101) proposes `ocm.software/localization` (`v1`, signing, a
  single `matchKey` with split registry, repository, tag and digest, written by
  `ocm add cv --localize-hints`). This document proposes
  `ocm.software/original-locations` (a list of references as written, written by
  default). One label should serve both; the convention matcher should be one library
  used at deploy time and at import.
* **Normalized OCI layout.** With PR #3888 the tag resolves to an access-free manifest
  and the accesses live in a referrer. The lockdown signature must then cover the
  access-bearing artifact, and that referrer's tag must work with immutable tags.
* **Verifier support**, so far read from code and not run: Flux source-controller
  discovers cosign v3 bundles and has an offline public-key path; Kyverno
  `ImageValidatingPolicy` verifies bundle referrers with a key attestor. A spike should
  confirm both, including that a key-signed image signature is a bundle with a DSSE
  envelope.
* Registries differ on re-pushing an identical manifest to an immutable tag.
* ocm-spec: `ociArtifactDigest/v1` on component references, the label, and the statement
  that a locked component version is an environment-bound derivative.
* Convention coverage: which chart corpus to measure against.
* Does transfer copy existing artifact signatures today, and does it overwrite an
  existing component version?
* Location report format.
* Multi-arch artifacts: sign the index only, or recursively?
* Should the replication controller ([ADR 0020](0020_replication_controller.md)) run
  imports?

## 8. Plan

1. **Spike**: registry with immutable tags and the Referrers API; sign an image and a
   chart with cosign and a key; verify with Flux and Kyverno; add a second signature.
2. **Original-locations label** in `ocm add cv`. Independent of lockdown and needed on
   the source side before signing.
3. **`SignOCIArtifact`** node and its verification counterpart.
4. **Lockdown primitive**: pinned accesses, bottom-up upload with manifest digests as
   reference digests, removal of invalidated signatures, signature on each stored
   descriptor, `ocm verify cv --lockdown`, transfer refusal.
5. **`ocm transfer cv --lockdown`** without localization: configuration entry, input
   validation, deep verification, location report.
6. **Localization** for Helm charts and plain manifests, built against a corpus of real
   charts.
7. **Reference scenario**: the sovereign scenario without OCM controllers.

Later: provenance record in the locked component version, kustomize overlays, notation,
repository-context binding.

## 9. Risks

* Localization coverage remains the hard problem. Lockdown moves the failure to import
  time; it does not remove it.
* Producer signatures are dropped from every non-leaf and every localized component
  version. Provenance depends on keeping the transport archive.
* A single importer key is a single point of compromise for the environment.
* User-authored Flux wiring gives up the self-contained delivery that a
  `ResourceGraphDefinition` provides.
* New public surface: a transfer flag and configuration type, a verification path, a
  predefined label.
