# Normalized OCI Layout for Cosign-Signable Component Versions

* **Status**: proposed
* **Deciders**: OCM Technical Steering Committee
* **Date**: 2026-10-12

Technical Story: Make an OCM component version stored in an OCI registry or CTF signable and verifiable with standard
cosign tooling, exactly like a container image, with the signature surviving `ocm transfer` between registries.
Design discussion: [open-component-model#3172](https://github.com/open-component-model/open-component-model/pull/3172).

## Context and Problem Statement

A component version stored in OCI ([ocm-spec OCI storage](https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/03-storage-backends/oci.md),
called "the OCI spec" below) is one OCI manifest (or index) whose descriptor layer is the full, access-bearing
component descriptor, plus one layer per local blob. The tag `<component>:<version>` resolves to that manifest.

When a component version is copied to another registry, its `access` specifications are rewritten (local
references point into the target, image references may be relocated) and the writer adds per-repository
annotations such as `software.ocm.creator`. The descriptor bytes and therefore the manifest digest change on every
copy. A signature over the manifest digest, such as a plain `cosign sign <ref>`, is invalidated by transport.

OCM solves this for its own signatures (ADR 0008 and ADR 0017): `jsonNormalisation/v4alpha1` strips access,
repository contexts, signatures and non-signing labels, and the signature over that normalised form is stored inside
the descriptor's `signatures[]`. That signature travels with the descriptor, but it is invisible to the OCI ecosystem:
`cosign verify`, policy controllers and admission webhooks that verify OCI referrers cannot use it.

What is missing is an OCI manifest that represents the component version, whose digest does not change on copy, and
that cosign can sign and verify like any image.

## Decision Drivers

* A cosign signature or attestation made on one registry must verify on any registry the component version was
  transferred to, without re-signing.
* Integrity must not depend on the unsigned, per-registry part of the representation: every byte OCM hands out must
  be bound to the signed digest.
* Existing component versions, repositories and readers must keep working; the new representation is opt-in and
  explicitly versioned.
* The OCM component version digest (the `jsonNormalisation/v4alpha1` digest that OCM signatures sign) must stay the
  single identity of a component version's content.
* Transfer behaviour is controlled by the transfer configuration (ADR 0018) and is visible as transformation nodes
  in the transfer graph (ADR 0005).

## Considered Options

* Option 1: Normalized tag target with an access referrer ("normalized layout").
* Option 2: Keep the v2 layout and verify Sigstore signatures only inside the descriptor (ADR 0017).
* Option 3: Keep the v2 layout and re-sign the manifest digest after every transfer.

## Decision Outcome

Chosen [Option 1](#option-1-normalized-layout): "Normalized tag target with an access referrer".

Justification:

* The digest the tag resolves to depends only on content that the OCM component version digest already covers, so
  it is identical in every registry and cosign signatures attached to it survive transfer.
* The normalized descriptor layer is byte-identical to the input of the OCM component version digest. A cosign
  signature over the manifest therefore covers exactly what an OCM signature covers, and the two mechanisms agree on
  the identity of a component version.
* The access-bearing part stays an ordinary OCM descriptor that existing code reads, but it is trusted only after a
  bind check against the signed layer.

### Option 1: Normalized Layout

#### Description

##### Layout selection

The layout is selected per repository and recorded on the stored manifest, never inferred from configuration alone
when reading.

* OCI and CTF repository specifications get the field `componentVersionLayout` with the values `v2` and `normalized`
  (Go type `layout.ComponentVersionLayout` in `bindings/go/oci/spec/layout`). An empty value means *auto*. Unknown
  values are rejected by the repository provider.
* `oci.Layout` has three values: `LayoutAuto` (the zero value), `LayoutV2` and `LayoutNormalized`. With `LayoutAuto`,
  `AddComponentVersion` resolves the version tag first: if it already points to a normalized manifest, the version
  is written in the normalized layout again, otherwise in v2. An explicit `v2` or `normalized` always wins.
  Read-modify-write commands such as `ocm sign` (which persists signatures by re-adding the descriptor) therefore
  keep a normalized version normalized and do not move its tag away from the signed digest.
* `ocm add component-version --layout v2|normalized` sets the layout of a newly added version. Without the flag, the
  layout of a version already stored at the tag is kept, otherwise v2 is used.
* Readers detect the layout from the `artifactType` of the manifest the tag resolves to.
  `repository.ComponentVersionLayoutDetector` (implemented by `*oci.Repository`) exposes the detected layout
  (`v2` or `normalized`) to callers such as the transfer graph builder.

##### Normalized manifest `M`

The tag resolves to an OCI image manifest `M` with digest `D_norm`:

| Field | Value |
| --- | --- |
| `artifactType` | `application/vnd.ocm.software.component-descriptor.normalized.v1` |
| `config` | `application/vnd.ocm.software.component.config.v1+json`, content `{"componentDescriptorLayer": <descriptor of N>}` |
| `layers` | exactly one layer `N`, media type `application/vnd.ocm.software.component-descriptor.normalized.v1+json` |
| `annotations` | `software.ocm.component-model/layout-version: v1`, `software.ocm.component-model/normalisation-algo: jsonNormalisation/v4alpha1`, `software.ocm.componentversion: <component>:<version>`, `org.opencontainers.image.version`, `org.opencontainers.image.title` |
| `subject` | the Component Index (`indexv1.Descriptor`) if and only if the repository uses `ReferrerTrackingPolicyByIndexAndSubject`, otherwise absent |

`N` is exactly `normalisation.Normalise(F, "jsonNormalisation/v4alpha1")` of the full descriptor `F`: a JCS-canonical
descriptor without `access`, `repositoryContexts`, `signatures`, `srcRefs` or non-signing labels, which keeps resource,
source and reference digests. Hence:

* `digest(N)` is the OCM component version digest (SHA-256 of the normalised bytes), the value OCM signatures sign.
* Every field of `M` is a deterministic function of the component version content and the referrer tracking policy.
  `M` carries no creator, author or timestamp annotations, so `D_norm` is identical in every repository that uses
  the same referrer tracking policy.

##### Access referrer `A`

The access-bearing descriptor `F` and the local blobs are stored in an *access referrer* `A` of `M`, regenerated per
repository and never signed:

* Without OCI-native local blobs, `A` is an image manifest: `artifactType`
  `application/vnd.ocm.software.component-descriptor.access.v1`, `subject` = `M`, config
  `application/vnd.ocm.software.component.config.v1+json`, layers = the descriptor layer `F`
  (`application/vnd.ocm.software.component-descriptor.v2+json`) followed by the local-blob layers.
* With local blobs that are OCI artifacts (OCI image layouts, Helm charts as OCI artifacts), `A` is an image index with
  the same `artifactType` and `subject` whose manifests are an inner access-descriptor manifest (the manifest above,
  without `subject`) and the manifests or indexes of the OCI-native local blobs. This mirrors the index
  representation of the v2 layout (ADR 0013), so OCI-native local blobs stay usable by OCI tools.
* `A` carries no `software.ocm.componentversion` annotation and is never listed as a component version.
* `A` is additionally tagged `sha256-<hex(D_norm)>.acc`, following cosign's `sha256-<hex>.sig` convention, for
  registries without the referrers API.

##### Read path

1. Resolve the tag to `M`. An `artifactType` of `...component-descriptor.v2` (or none) selects the v2 path, unchanged.
   `...component-descriptor.normalized.v1` selects the normalized path. Manifests and indexes with the access
   `artifactType` are rejected as component version roots.
2. Read `N` from `M.layers[0]`.
3. Select the access referrer:
   * If the tag `sha256-<hex(D_norm)>.acc` resolves, its target is authoritative. It must have the access
     `artifactType`, `subject` = `M`, pass the bind check and the digest rules below; otherwise reading fails. There is
     no fall-through to other referrers, so a pushed extra referrer cannot win over the tagged one.
   * Only if the tag does not exist, collect the `access.v1` referrers of `M` (`Predecessors`). Candidates that fail
     the bind check or the digest rules are discarded. The survivors must have byte-identical descriptor layers,
     otherwise reading fails as ambiguous. Among identical survivors the lowest manifest digest is chosen only for
     determinism.
4. **Bind check**: `normalise(F, jsonNormalisation/v4alpha1)` must be byte-identical to `N`. On mismatch, reading
   fails with an error that names possible tampering.
5. **Digest rules** on the bind-checked `F`: the rules of `signing.IsSafelyDigestible` and
   `signing.ValidateDigestHashAlgorithms`. Every component reference carries a complete digest; every resource with a
   usable access carries a complete digest; a resource without access (`nil` or `None`) carries no digest; digests use
   SHA-256 or SHA-512 (legacy spellings such as `sha256` accepted), unless they are explicitly excluded from signing
   (`NO-DIGEST` / `EXCLUDE-FROM-SIGNATURE`). Because `access` is not covered by `N`, these rules are what prevents a
   referrer from attaching a usable access to content nobody signed.
6. Return `F`.

Local resources and sources are resolved only among the descriptors of the selected `A` (`A.layers`, or the inner
manifest's layers plus `A.manifests` for the index shape), exactly as the v2 path resolves among the descriptors of
the tagged artifact (OCI spec §10.3). A `localReference` that is not part of the selected `A` does not resolve, even
if the blob exists elsewhere in the repository. Resource content is verified against the signed resource digest:
a nested OCI manifest or index must have the `ociArtifactDigest/v1` digest (or the legacy `genericBlobDigest/v1`
label for OCI artifacts), and a plain layer is streamed through `repository.VerifyDownload`. Resources excluded from
signing are not verified. Sources carry no digest in OCM, so only the scoping applies to them.

##### Write path

1. Validate `F` against the digest rules; publishing fails otherwise.
2. With `ReferrerTrackingPolicyByIndexAndSubject`, ensure the Component Index exists (OCI spec §8.5.1).
3. Push `N`, the config and `M`, and tag `<version>` → `M`.
4. Push the local blobs, `F` and `A` with `subject` = `M`, and tag `sha256-<hex(D_norm)>.acc` → `A`.

Re-adding a version with unchanged signed content (for example `ocm sign`, which only adds to `signatures[]`)
produces the same `M` and a new `A'`. Older access referrers are not deleted; the `.acc` tag moves to `A'` and is
authoritative on read, so the most recently written descriptor is returned.

Listing component versions (`ListComponentVersions`) accepts both the v2 and the normalized `artifactType` in the
Component Index referrers, identifies normalized versions through their `software.ocm.componentversion` annotation,
and never lists `.acc` tags or access referrers as versions.

##### Transfer

* `transferv1alpha1.Config` (`transfer.config.ocm.software/v1alpha1`) gets `componentVersionLayout` (`v2` or
  `normalized`). The effective layout of each component version on each OCI or CTF target is, in order of precedence:
  the transfer configuration, the `componentVersionLayout` of the target repository specification, the layout
  detected on the source component version (through `repository.ComponentVersionLayoutDetector`), and finally v2.
  A normalized source therefore stays normalized unless the user explicitly asks for v2. The chosen layout is
  written into the target repository specification of every node of that component version, so `--dry-run` output
  and stored transfer specifications are self-contained.
* `ocm transfer component-version --layout v2|normalized` sets the configuration entry (the flag wins over a
  configuration file). With `--transfer-spec` the layout is already part of the specification and the flag is
  ignored with a warning.
* For every component version whose effective target layout is normalized and whose source is an OCI or CTF
  repository, the graph contains a separate node `<id>Carry` of type `OCICarryComponentSignatures/v1alpha1` or
  `CTFCarryComponentSignatures/v1alpha1` after the upload node `<id>Upload`. Its specification has the fields
  `repository` (target), `sourceRepository`, `component` and `version`. It runs
  `repository.ComponentSignatureCarrier.CarryComponentSignatures`, which copies the referrers of the source `M`
  (`Predecessors`: the OCI referrers API with the referrers tag schema `sha256-<hex>` as fallback, or the CTF graph),
  except access referrers, to the target `M`. This covers cosign v3 bundles and other OCI 1.1 referrers; signatures
  that cosign stores only under its legacy tag scheme (`sha256-<hex>.sig`, `.att`) are not copied.
  `sourceRepository` is no longer part of the `AddComponentVersion` specifications.
* The carry is strict. It is a no-op (with an info log) only when the source version is not in the normalized layout
  or the source is not an OCI or CTF repository. A digest mismatch between source and target `M`, a failed referrer
  discovery, or a failed copy fail the node and therefore the transfer, so a user who opted into the normalized
  layout never gets a target without the signatures of the source.
* `D_norm` includes the `subject` of `M`. Source and target must therefore use the same referrer tracking policy
  (the default, `None`, everywhere including CTF); otherwise the carry fails on the digest mismatch and the component
  version has to be signed again on the target.

##### Pinned component references

Digest pinning of component references is introduced separately in
[#3889](https://github.com/open-component-model/open-component-model/pull/3889), with two forms:
`@<hash>:<hex>` pins the descriptor bytes as stored (repository-specific), and
`@<normalisation>+<hash>:<hex>` (for example `@jsonNormalisation.v4alpha1+sha256:<hex>`, with `/` written as `.`)
pins the OCM component version digest, which stays the same across transfer and signing. OCM commands fail unless
the resolved descriptor matches, and downloads verify content against the pinned descriptor. This ADR relies only on
the normalized form: for the normalized layout and SHA-256, the component version digest is `digest(N)`, which is
`layers[0].digest` of `M`.

##### Trust model

* The bind check and the digest rules give **integrity relative to `M`**: everything OCM returns is bound to `N`.
  They do not give authenticity: anyone who can push to the repository can also retag `<version>`.
* **Authenticity** comes from verifying a signature over `D_norm` and then using exactly that content, through
  `cosign verify` and the digest pinning of
  [#3889](https://github.com/open-component-model/open-component-model/pull/3889):

  ```bash
  cosign verify --key cosign.pub <registry>/<path>/component-descriptors/<component>@<D_norm>
  # take layers[0].digest (sha256:<hex>) of the verified manifest
  ocm download resource "<repository>//<component>:<version>@jsonNormalisation.v4alpha1+sha256:<hex>" --identity name=<resource>
  ```

  `cosign verify` establishes that `M` with digest `D_norm` is signed. `layers[0].digest` of that manifest is the
  digest of `N`, which is the OCM component version digest. Pinning the OCM command to it with the normalized pin
  form binds what OCM reads to the verified manifest without trusting the tag a second time.
* Verifying cosign signatures inside OCM, through the cosign executor of ADR 0017 as a verifier for the normalized
  layout, is future work. Until then, a tag-only reference to a normalized component version gives integrity only.
* OCM signatures in `signatures[]` remain available for both layouts and are independent of this ADR.

##### Behaviour of older readers

Released readers that follow OCI spec §7 interpret `M` as a component descriptor manifest: they find the config
`componentDescriptorLayer`, fetch `N`, and fail cleanly on its unknown media type (`unsupported media type`). Readers
that expect a tar-encoded descriptor fail to decode it. No reader misinterprets `N` as an access-bearing descriptor
or dereferences a missing layer. Writers that predate this ADR overwrite the tag with a v2 manifest when they re-add
the version; this moves the tag away from `D_norm`, as any v2 re-publish would.

#### High-level Architecture

```text
tag <component>:<version>                tag sha256-<hex(D_norm)>.acc
          │                                          │
          ▼                                          ▼
┌───────────────────────────────┐ subject ┌──────────────────────────────────┐
│ M  (D_norm)  normalized.v1    │◄────────┤ A  access.v1 (manifest or index) │
│ config: componentDescriptor-  │         │ layers: F (+ local blobs)        │
│         Layer → N             │         │ [index: inner manifest + OCI     │
│ layers: [N]                   │         │  local-blob manifests]           │
└───────────────▲───────────────┘         └──────────────────────────────────┘
                │ subject
        ┌───────┴────────┐
        │ cosign         │   copied by the <id>Carry transfer node
        │ signatures,    │
        │ attestations   │
        └────────────────┘
```

#### Contract

* Go: `layout.ComponentVersionLayout` (`v2`, `normalized`, empty = auto); `oci.Layout` (`LayoutAuto`, `LayoutV2`,
  `LayoutNormalized`); `repository.ComponentVersionLayoutDetector`; `repository.ComponentSignatureCarrier`.
* Specifications: `componentVersionLayout` on `OCIRepository/v1` and `CommonTransportFormat/v1`;
  `componentVersionLayout` on `transfer.config.ocm.software/v1alpha1`; transformations
  `OCICarryComponentSignatures/v1alpha1` and `CTFCarryComponentSignatures/v1alpha1`.
* Media types and annotations as listed above; constants in `bindings/go/oci/spec/descriptor` and
  `bindings/go/oci/spec/layout/normalized/v1`.

### ocm-spec Amendment

The following normative text is proposed for the OCI storage document of ocm-spec. Section numbers refer to that
document.

#### §6.3 (new) Normalized Representation

A component version **MAY** be stored in the normalized representation. A normalized component version consists of
a normalized manifest and one or more access referrers.

The normalized manifest:

* **MUST** be an image manifest with `artifactType`
  `application/vnd.ocm.software.component-descriptor.normalized.v1`.
* **MUST** contain exactly one layer with media type
  `application/vnd.ocm.software.component-descriptor.normalized.v1+json`, whose content is the output of
  `jsonNormalisation/v4alpha1` applied to the component descriptor.
* **MUST** include a config with media type `application/vnd.ocm.software.component.config.v1+json` whose
  `componentDescriptorLayer` references that layer.
* **MUST** carry the annotations `software.ocm.component-model/layout-version` (`v1`),
  `software.ocm.component-model/normalisation-algo` (`jsonNormalisation/v4alpha1`) and `software.ocm.componentversion`.
* **MUST NOT** carry annotations or fields whose values depend on the repository, the writer or the time of writing.
* **MUST** carry the Component Index as `subject` if the writer applies a referrer tracking policy that requires it
  (§8.4), and **MUST NOT** carry a `subject` otherwise.

An access referrer:

* **MUST** be an image manifest or an image index with `artifactType`
  `application/vnd.ocm.software.component-descriptor.access.v1` and the normalized manifest as `subject`.
* **MUST** contain the full component descriptor, with access information valid for the repository, in the manifest
  representation of §6.1 (as the manifest itself, or as a manifest of the index).
* **MUST** contain every `localBlob` of that descriptor as a layer of that manifest or as a manifest or index of the
  index, following §6.2.
* **MUST** be tagged `sha256-<hex>.acc`, where `<hex>` is the hex digest of the normalized manifest.
* **MUST NOT** carry the `software.ocm.componentversion` annotation.

Writers **MUST** reject a component descriptor for the normalized representation unless every component reference
carries a complete digest, every resource with an access other than `None` carries a complete digest, no resource
without access carries a digest, and all of these digests use SHA-256 or SHA-512, except digests explicitly excluded
from signing.

#### §7 Descriptor Selection Logic (amended)

Before step 2, a reader **MUST** inspect the `artifactType` of the resolved manifest:

* `application/vnd.ocm.software.component-descriptor.normalized.v1`: the reader **MUST** select the access
  referrer as follows and **MUST** fail if none is selected:
  1. If the tag `sha256-<hex>.acc` exists, its target **MUST** be selected. Resolution **MUST** fail if it does not
     have the access `artifactType` and the normalized manifest as `subject`, or if it fails the checks below.
  2. Otherwise the reader **MUST** consider all access referrers of the normalized manifest, discard those that fail
     the checks below, and **MUST** fail if the remaining candidates do not contain byte-identical descriptors.
* The checks are: `jsonNormalisation/v4alpha1` applied to the descriptor of the access referrer **MUST** be
  byte-identical to the normalized layer, and the descriptor **MUST** satisfy the digest rules of §6.3.
* `application/vnd.ocm.software.component-descriptor.access.v1`: the artifact **MUST NOT** be interpreted as a
  component version root.
* Readers that do not support the normalized representation **MUST** fail resolution instead of interpreting the
  normalized layer as a component descriptor.

#### §8 Component Index (amended)

* §8.5.2: The normalized manifest takes the role of the top-level manifest; access referrers **MUST NOT** carry the
  Component Index as `subject`.
* §8.5.4: Version enumeration through referrers of the Component Index **MUST** accept both the
  `application/vnd.ocm.software.component-descriptor.v2` and the
  `application/vnd.ocm.software.component-descriptor.normalized.v1` `artifactType`.

#### §10 `localBlob` Processing (amended)

* §10.3: For the normalized representation, the descriptors reachable from the component version artifact are the
  descriptors reachable from the selected access referrer. A `localReference` **MUST NOT** be resolved against other
  content of the repository.
* §10.4 (new rule): For the normalized representation, retrieved content **MUST** be verified against the resource
  digest of the selected descriptor before it is returned as verified content; a mismatch **MUST** fail retrieval.

#### §12 Tag and Version Mapping Rules (amended)

* Tags matching `sha256-<hex>.acc` are access tags. They **MUST NOT** be considered component versions and
  **MUST NOT** be used as aliases (§12.1).
* A component version reference with a normalized digest pin `@<normalisation>+<hash>:<hex>` (digest pinning,
  [#3889](https://github.com/open-component-model/open-component-model/pull/3889)) names the component version
  digest. For the normalized representation with `jsonNormalisation.v4alpha1+sha256`, this digest equals the digest
  of the normalized layer.

## Pros and Cons of the Options

### [Option 1] Normalized Layout

Pros:

* Standard cosign signing, verification and attestation work on component versions, and signatures survive transfer.
* The cosign-signed content and the OCM-signed content are the same bytes, so a single digest identifies the content
  in both worlds and can be used to pin OCM commands.
* Local blobs, including OCI-native ones, stay in the repository; the v2 index representation is reused for them.

Cons:

* A second OCI representation that readers have to support; older readers cannot read normalized component versions.
* Reading needs an extra round trip for the access referrer.
* `D_norm` depends on the referrer tracking policy, so signatures are only portable between repositories that use
  the same policy.
* Access referrers written by earlier re-adds remain in the repository until garbage-collected by other means.

### [Option 2] In-Descriptor Sigstore Signatures Only

Pros:

* No new representation; already implemented (ADR 0017).
* Signatures travel inside the descriptor through any transport, including non-OCI repositories.

Cons:

* Invisible to OCI-native verification (cosign, policy controllers), which is the goal of this ADR.

### [Option 3] Re-Sign After Transfer

Pros:

* No change to the storage format.

Cons:

* Every target environment needs signing keys or identities, which breaks the producer-signs, consumer-verifies model
  and is not possible in many air-gapped setups.

## Discovery and Distribution

* The library implements the layout in `bindings/go/oci`; the transfer graph and the CLI expose it as described above.
* The CLI reference and the concept documentation on ocm.software describe the layout and transfer behaviour; the
  `conformance/scenarios/cosign-normalized` scenario exercises it end to end with two registries.
* The ocm-spec amendment above is proposed to the ocm-spec repository.
* Future work: verification of cosign signatures on `D_norm` inside OCM through the cosign executor of ADR 0017, and
  cleanup of superseded access referrers.

## Conclusion

The normalized layout makes the digest a tag resolves to a function of the signed content only. Standard cosign
signatures and attestations on component versions therefore survive transfer, are carried by an explicit transfer
node, and can be tied to OCM commands by pinning the OCM component version digest (#3889). The v2 layout stays the
default and is unchanged.
