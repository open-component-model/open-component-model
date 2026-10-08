# Navigation Restructure Proposal

## Goal

Merge the current **How-to Guides** and **Tutorials** sections into a single
section, introduce lifecycle-phase sub-groups only inside that merged section,
and rename documents so their titles fit naturally within the phase they belong
to. All other top-level sections (Overview, Getting Started, Concepts,
Reference) remain structurally unchanged.

---

## Baseline

This proposal describes changes against `main` of
`github.com/open-component-model/open-component-model`. On `main` the docs are
**flat**: every how-to lives directly under `how-to/` and every tutorial
directly under `tutorials/`. There are no lifecycle-phase sub-directories yet.
Four directories are the only existing nesting:

- `how-to/Sign and Verify/` (note the spaces) holds the four signing how-tos.
- `how-to/configure-http/` holds one `_index.md` plus five task pages.
- `how-to/vendor-specific-apis/` holds a two-level product/format tree.
- `tutorials/signing/` holds the four signing tutorials.

All `was:` paths below are the real current paths on `main`.

---

## Proposed Top-Level Navigation

```
Overview
Getting Started
Concepts
Guides          ← merged How-to + Tutorials (new name)
Reference
```

**Why "Guides"?** The merged section contains both task-focused how-tos and
deeper explorations through concrete examples. "Guides" is the common noun that
covers both without forcing the Diátaxis vocabulary on the reader.
Alternatives: "Hands-on", "Cookbook", "Practical Guides", but "Guides" is
shortest and most conventional.

---

## Proposed Structure of the Affected Sections

Getting Started and Concepts are shown alongside the merged Guides section:
Getting Started gains one page moved in from How-to, and Concepts gains one page
extracted from the old transfer credential tutorial. Both are otherwise unchanged.

```
Getting Started  [getting-started/]
├── Install the OCM CLI                             [ocm-cli-installation.md]                   unchanged (binary install; paired with the container-image page below)
├── Run the OCM CLI from a Container Image          [run-cli-from-container-image.md]           was: "How to use the OCM CLI container image" in how-to/container-image-usage.md  ← MOVED IN (phase-agnostic; retitled to pair with "Install the OCM CLI")
├── Create Component Versions                       [create-component-version.md]               unchanged (essential first hands-on step; stays in Getting Started)
├── Set Up Your Controller Environment              [setup-controller-environment.md]           was: "Set up Controller Environments" (retitled; stays in Getting Started)
└── Deploy Helm Charts                              [deploy-helm-chart.md]                      unchanged

Guides  [guides/]
├── pack/
│   ├── Add Component References                     [add-component-references.md]               was (partial): "Working with Resolvers" in tutorials/configure-resolvers.md  ← SPLIT, pack half, scope broadened to same-repo refs
│   ├── Add Git Repositories                        [add-git-repositories.md]                   was: "Working with Git Repositories" in tutorials/working-with-git.md (slug renamed working-with-git.md → add-git-repositories.md to match the new title and the sibling add-* pack guides; old URL aliased)
│   ├── Add HTTP Resources                          [add-http-resources.md]                     was: "Add Resources from HTTP URLs" (how-to/add-resources-from-http-urls.md) + "Working with HTTP Resources" (tutorials/wget-http-resources.md), MERGED
│   ├── Add OCI Artifacts                           [add-oci-artifacts.md]                      was (partial): "Working with OCI" in tutorials/working-with-oci/_index.md  ← SPLIT, pack half
│   ├── Add Resources from GitHub                   [add-resources-from-github.md]              was: "Add Resources from GitHub" in how-to/add-resources-from-github.md
│   ├── Add SBOMs                                   [add-sboms.md]                              was (partial): "Working with SBOMs" in tutorials/working-with-sboms.md  ← SPLIT, pack half
│   ├── Add and Verify Ownership Information        [add-and-verify-ownership.md]               was: "Add and Verify Ownership Information" in how-to/add-and-verify-ownership.md  (stays whole, see analysis)
│   ├── Compose a Multi-Component Product           [compose-multi-component-product.md]        was: "Create a Multi-Component Product" in tutorials/advanced-component-constructor.md  ("Create Component Versions" in Getting Started is the prerequisite. The simpler how-to/model-products.md is DROPPED, its old URL redirects here)
│   └── Configure a Versioning Scheme               [configure-versioning.md]                   was: "Configure a Versioning Scheme" in tutorials/configure-versioning.md
├── sign/
│   ├── Generate Signing Keys                       [generate-signing-keys.md]                  was: "Generate Signing Keys" in how-to/Sign and Verify/generate-signing-keys.md
│   ├── Configure Signing Credentials               [configure-signing-credentials.md]          was: "Configure Credentials for Signing" in how-to/Sign and Verify/configure-signing-credentials.md
│   ├── Sign Component Versions                     [sign-component-version.md]                 was: "Sign Component Versions" in how-to/Sign and Verify/sign-component-version.md  (overview of signing; the four "Sign with …" pages below are method-specific)
│   ├── Sign with GPG                               [sign-with-gpg.md]                          was: "GPG Signatures" in tutorials/signing/gpg.md
│   ├── Sign with a PEM Certificate Chain           [sign-with-pem-certificate.md]              was: "Certificate Chains (PEM)" in tutorials/signing/pem.md
│   ├── Sign with Plain RSA                         [sign-with-plain-rsa.md]                    was: "Plain Signatures" in tutorials/signing/plain.md
│   ├── Sign with Sigstore (Keyless)                [sign-with-sigstore.md]                     was: "Sigstore (Keyless)" in tutorials/signing/sigstore.md
│   └── Verify Component Versions                   [verify-component-version.md]               was: "Verify Component Versions" in how-to/Sign and Verify/verify-component-version.md
├── transfer/
│   ├── Transfer Component Versions                 [transfer-component-versions.md]            NEW: base transfer guide (default `ocm transfer cv <source> <target>`; CTF↔OCI-registry source/target; metadata-only default vs self-contained local-blob copy; prerequisites, credentials, verify). Entry point the air-gap/Helm/vendor pages specialize. Content assembled from concepts/transfer-concept.md + the shared boilerplate currently duplicated across the vendor/air-gap pages
│   ├── Transfer Components across an Air Gap       [air-gap-transfer.md]                       was: "Transfer Components across an Air Gap" in how-to/air-gap-transfer.md
│   ├── Transfer Components with Helm Charts        [transfer-helm-charts.md]                   was: "Transfer Helm Charts with OCM" in how-to/transfer-helm-charts.md
│   ├── Transfer Components Continuously            [replicate-component-versions.md]           was: "Replicate Component Versions with the Controller" in how-to/replicate-component-versions-controller.md  (moved from deploy: continuous repo-to-repo transfer via the Replication controller; re-runs on each new source version. Slug keeps the Replication CRD name)
│   ├── Download Resources                          [download-resources.md]                     was: "Download Resources from Component Versions" in how-to/download-resources-from-component-versions.md
│   ├── Download SBOMs                              [download-sboms.md]                         was (partial): "Working with SBOMs" in tutorials/working-with-sboms.md  ← SPLIT, transfer half
│   ├── Pull OCI Artifacts Natively                 [pull-oci-artifacts-natively.md]            was (partial): "Working with OCI" in tutorials/working-with-oci/_index.md  ← SPLIT, transfer half
│   ├── Resolve Component References                [resolve-component-references.md]           was: "Working with Resolvers" (tutorials/configure-resolvers.md, transfer half) + "Resolving Components across Multiple Registries" (how-to/resolve-components-from-multiple-repositories.md), MERGED  ← SPLIT, transfer half
│   ├── Configure Registry Credentials              [configure-registry-credentials.md]        was: "Configure Credentials for Multiple Registries" in how-to/configure-multiple-credentials.md (slug renamed configure-multiple-credentials.md → configure-registry-credentials.md to match the new title; old URL aliased)
│   ├── Configure HTTP Behaviour                    [configure-http.md]                         was: 6 pages under how-to/configure-http/ (merged)
│   ├── Upload OCI Images                           [upload-oci-images.md]                      NEW: upload regular OCI image resources as directly-pullable OCI artifacts via the OCI uploader (oci.uploader.transfer.config.ocm.software/v1alpha1): default match (OCIImage/Helm/LocalBlob-OCI-manifest on OCI targets), imageReference templating, replaces deprecated `--upload-as ociArtifact`. Links to reference/transfer-configuration/oci-uploader.md and to "Pull OCI Artifacts Natively" for the consume direction
│   ├── Upload to a Custom Target                   [upload-to-custom-target.md]                was: "Configure Custom Uploads During Transfer" in tutorials/configure-custom-uploads.md
│   ├── Upload to JFrog Artifactory                 [upload-to-jfrog-artifactory.md]            was: "JFrog Artifactory" (how-to/vendor-specific-apis/jfrog-artifactory/_index.md) + its 4 format pages (helm-charts, maven-artifacts, npm-packages, generic-files), MERGED into per-format sections
│   ├── Upload to Sonatype Nexus                    [upload-to-sonatype-nexus.md]               was: "Sonatype Nexus" (how-to/vendor-specific-apis/sonatype-nexus/_index.md) + its 4 format pages (helm-charts, maven-artifacts, npm-packages, raw-files), MERGED into per-format sections
│   ├── Migrate Legacy Resolvers                    [migrate-legacy-resolvers.md]               was: "Migrate from Fallback to Deterministic Repository Resolvers" in how-to/migrate-from-deprecated-resolvers.md (slug renamed migrate-from-deprecated-resolvers.md → migrate-legacy-resolvers.md to match the new title and the sibling migrate-legacy-credentials.md; old URL aliased)
│   ├── Migrate Legacy Credentials                  [migrate-legacy-credentials.md]             was: "Migrate Legacy Credentials" in how-to/legacy-credential-compatibility.md
│   └── Migrate --upload-as Flags                   [migrate-from-upload-as.md]                 was: "Migrate from --upload-as to Uploader Configurations" in how-to/migrate-from-upload-as.md
└── deploy/
    ├── Configure Controller Credentials            [configure-controller-credentials.md]       was: "Configure Credentials for OCM Controllers" in how-to/configure-credentials-ocm-controllers.md
    ├── Configure Custom RBAC                       [configure-custom-rbac.md]                  was: "Configure Custom RBAC for Deployers" in how-to/custom-rbac.md (slug renamed custom-rbac.md → configure-custom-rbac.md to match the new title and the sibling configure-* guides; old URL aliased)
    ├── Discover Component Graphs                    [discover-component-graphs.md]              was: "Discover Component Graphs" in how-to/discover-component-graphs.md  (moved from transfer: a Discovery-controller operation needing a controller environment and a Ready Component, publishing to Kubernetes status; belongs with the deploy/controller phase)
    ├── Deploy with Chained kro RGDs                 [deploy-chained-rgds.md]                    was: "Deploy an Application from Chained RGDs with OCM and kro" in tutorials/deploy-chained-rgds.md
    ├── Deploy a Helm Chart with kro and GitOps     [deploy-helm-chart-with-kro.md]             was: "Deploy an Application from a Helm Chart with OCM and kro" in tutorials/deploy-helm-chart-bootstrap.md
    ├── Deploy Kubernetes Manifests with the OCM Deployer  [deploy-with-manifests.md]            was: "Deploy Manifests with Deployer" in how-to/deploy-manifests-with-deployer.md
    └── Verify Component Signatures on Deployment   [verify-signatures-in-controller.md]        was: "Verify Component Versions in the Controller" in how-to/verify-component-version-controller.md

Concepts  [concepts/]   (two new pages; resolvers.md lightly extended)
├── … 11 other concept pages unchanged (SBOMs, Transfer and Transport, Ownership, Component Identity, …)
├── Vendor Repository Uploaders                     [vendor-uploaders.md]                       ← NEW, extracted from the "How vendor uploaders work" sections duplicated in both vendor _index.md pages (match → first-match-wins, server-side repo-type detection, access rewrite to Helm/v1 or Wget/v1, genericBlobDigest/v1 verification, local/hosted-only)
├── Resolvers                                       [resolvers.md]                              lightly extended: absorbs the matching mechanics (location-free references, first-match ordering, glob syntax, versionConstraint, specificity) lifted out of the two resolver how-tos
├── Credential System                               [credential-system.md]                      unchanged (the why/what overview; shown for context)
└── Credential Resolution                           [credential-resolution.md]                  ← NEW, extracted from "Understand Credential Resolution" in tutorials/credential-resolution.md (the matching mechanics; the transfer tutorial drops out of Guides)

Reference  [reference/]   (structure unchanged; two existing uploader pages gain content, no new pages)
├── … all current Reference pages unchanged (component-descriptor, component-constructor, credential-*, http-client-configuration, input-and-access-types, kubernetes-api/*, resolver-configuration, standards-and-regulations/*, versioning-configuration, and the rest of transfer-configuration/* including the already-existing oci-uploader.md and http-uploader.md the new upload guides link down to)
└── transfer-configuration/
    ├── Artifactory Uploader                        [artifactory-uploader.md]                   extended (no retitle): gains the per-format repository-behaviour facts lifted out of the collapsed "Upload to JFrog Artifactory" guide; the page already carries the per-type tables
    └── Nexus Uploader                              [nexus-uploader.md]                         extended (no retitle): gains the per-format repository-behaviour facts (never-overwrite semantics, Maven snapshot routing, components-API npm upload) lifted out of the collapsed "Upload to Sonatype Nexus" guide
```

Ownership (`add-and-verify-ownership.md`) stays whole in pack (see analysis
below): the "verify" half is a
two-command `oras discover` readback against the same registry and digest the
author just pushed, too small and too tightly coupled to the push to justify a
second page.

**Sign phase, overview plus method variants:** "Sign Component Versions"
[`sign-component-version.md`] is the general signing guide (key-based or keyless,
encoding policy, troubleshooting) and acts as the entry point for the four
method-specific pages that follow it: "Sign with GPG" [`sign-with-gpg.md`],
"Sign with a PEM Certificate Chain" [`sign-with-pem-certificate.md`], "Sign with
Plain RSA" [`sign-with-plain-rsa.md`], and "Sign with Sigstore (Keyless)"
[`sign-with-sigstore.md`]. The order is deliberate: prerequisites (keys
[`generate-signing-keys.md`], credentials [`configure-signing-credentials.md`]),
then the general procedure, then the per-method walkthroughs, then "Verify
Component Versions" [`verify-component-version.md`] last so the whole signing
cluster stays together. The
variants are kept as flat siblings rather than nested under the overview to
respect the max-depth rule.

**`vendor-specific-apis/` collapsed into two flat guides:** on `main` this
subtree is a two-level product → format tree, 11 pages in all (one vendor
`_index.md` plus four format pages each, with the top `_index.md` "Using
Vendor-Specific APIs" overview). The eight format pages are ~90% identical: the
same `.ocmconfig` credentials block, the same `ocm transfer cv` and `ocm get cv`
steps, the same prerequisites and troubleshooting links back to the vendor
`_index.md`. The genuinely per-format content is small (one `match`/`path`
snippet and one consumer command).

The restructure removes the subtree, and with it the last max-depth exception,
by moving the content three ways:

- **Two flat guides**, "Upload to JFrog Artifactory"
  [`upload-to-jfrog-artifactory.md`] and "Upload to Sonatype Nexus"
  [`upload-to-sonatype-nexus.md`]. Each writes the shared scaffold once
  (prerequisites, configure credentials, run transfer, verify, troubleshooting)
  and carries one short section per artifact format (Helm charts, Maven
  artifacts, npm packages, generic/raw files) holding only the per-format
  `match`/`path` snippet and consumer command.
- **One new concept**, "Vendor Repository Uploaders" [`concepts/vendor-uploaders.md`],
  holds the server-side model that both vendor `_index.md` pages duplicate today
  (type/format read from the server, upload the way the type expects, access
  rewrite to `Helm/v1` or `Wget/v1`, `genericBlobDigest/v1` verification,
  first-match-wins, local/hosted-only).
- **Extended reference.** The per-format repository behaviour currently stranded
  in the guides (Maven `latest`/`release` and snapshot handling, npm `latest`
  dist-tag movement, Nexus never-overwrites semantics, Helm layout-enforcement
  caveat) moves into the existing
  `reference/transfer-configuration/{artifactory-uploader,nexus-uploader}.md`
  pages, which already carry the per-type tables.

The top overview `_index.md` is dropped: its "How vendor uploaders work" section
becomes the new concept, and its product list is covered by the two guides.

---

## New Landing Pages

The merged section needs an `_index.md` for itself and one per phase, so the nav
renders section headers and each phase has an intro:

- `guides/_index.md` (new, replaces the two current `how-to/_index.md` and
  `tutorials/_index.md` section intros)
- `guides/pack/_index.md` (new)
- `guides/sign/_index.md` (new)
- `guides/transfer/_index.md` (new)
- `guides/deploy/_index.md` (new)

The existing `how-to/configure-http/_index.md` and the three
`how-to/vendor-specific-apis/**/_index.md` pages are absorbed: the first becomes
the single merged `configure-http.md`; the top vendor overview folds into the
new `concepts/vendor-uploaders.md`, and each vendor `_index.md` plus its four
format pages collapses into one flat guide.

---

## Split Design: Cross-Link Handoff

Three documents (SBOMs, OCI, Resolvers) are split at their phase boundary. Each
produces a **pack half** and a **transfer half**. The halves are chained, not
duplicated:

- The **pack half** is the entry point. It runs the authoring narrative and ends
  with a handoff callout pointing to the transfer half. It holds the **primary**
  alias for the original URL.
- The **transfer half** opens with a "continues from" banner naming the pack half
  as a required prerequisite, then resumes exactly at the phase-boundary command.
  It does **not** repeat the workspace, constructor, or `ocm add cv` setup. It
  holds a **secondary** alias for the original URL.

Consequence to accept: the transfer half is **not independently runnable**. A
reader who lands on it directly must follow the banner back to the pack half and
complete it first. This is the explicit tradeoff chosen over duplicating setup
(which would drift out of sync) or keeping the documents whole.

### Split points

| Document | Pack half ends at | Transfer half resumes at | Shared state carried across |
|---|---|---|---|
| "Working with SBOMs" | after "verify the label is in the descriptor" (`ocm get cv ./transport-archive`) | "Retrieve the linked SBOM" (`ocm download resource --sbom`) | `/tmp/ocm-sbom-tutorial` workspace + `./transport-archive` CTF |
| "Working with OCI" | end of each tab's `ocm add cv` + CTF inspection | each tab's "Transfer with `--copy-resources`" step | per-tab `/tmp` workspace + the built component version |
| "Working with Resolvers" | the callout "Do not call `add cv` yet" (components created, not pushed) | "Recursively Resolve the App with Resolvers" (`.ocmconfig` resolver block) | `/tmp/ocm-resolver-tutorial` workspace + the shared `.ocmconfig` |

The resolvers transfer half is also where the multi-repo how-to merges in: after
the single-repo walkthrough it carries the realistic separate-repositories case
(one resolver entry per registry). The combined page is "Resolve Component
References" [`resolve-component-references.md`].

**OCI two-tab handling (decided):** the source document
(`tutorials/working-with-oci/_index.md`) has two independent tabs
("Embed an OCI Image Layout" and "Transfer by Value"), each a full embed →
transfer → access flow with its own `/tmp` workspace. Both tabs are preserved on
each half: the pack half "Add OCI Artifacts" [`add-oci-artifacts.md`] keeps both
tabs' create/embed steps, the transfer half "Pull OCI Artifacts Natively"
[`pull-oci-artifacts-natively.md`] keeps both tabs' transfer/access steps. Each
tab's handoff is independent, so the pack half ends with two "Continues in …"
callouts (one per tab) and the transfer half opens with two "continues from"
banners (one per tab), each naming its tab's workspace.

### Handoff callout wording (pack half, example for SBOMs)

```md
{{< callout context="note" title="Continues in Download SBOMs" >}}
You now have a component version with a linked SBOM in `./transport-archive`.
To retrieve, collect, scan, and verify SBOMs survive a transfer, continue with
[Download SBOMs]({{< relref "docs/guides/transfer/download-sboms.md" >}}).
Keep this terminal and workspace open, the next guide picks up where this one ends.
{{< /callout >}}
```

### Prerequisite banner wording (transfer half, example for SBOMs)

```md
{{< callout context="caution" title="Complete the pack guide first" >}}
This guide continues from
[Add SBOMs]({{< relref "docs/guides/pack/add-sboms.md" >}}).
It assumes the `/tmp/ocm-sbom-tutorial` workspace and the `./transport-archive`
CTF from that guide already exist. Complete it before running the steps below.
{{< /callout >}}
```

---

## Title Changes

All titles follow a single rule: **imperative verb phrase naming what the reader
does**. Phase membership is implied by the sub-section, so titles do not need to
repeat it.

### Getting Started

| Current title | Proposed title | Reason |
|---|---|---|
| "How to use the OCM CLI container image" | **"Run the OCM CLI from a Container Image"** | Retitled to pair with "Install the OCM CLI": both lead with "the OCM CLI" and sit adjacent in Getting Started, so the reader reads them as two ways to get a working CLI (install a binary, or run from a container). Also moves out of How-to because running the CLI from a container is phase-agnostic (every phase invokes `ocm`). Filename `run-cli-from-container-image.md` matches the new title |
| "Set up Controller Environments" | **"Set Up Your Controller Environment"** | "Your" adds reader context; singular matches single-cluster scope. Stays in Getting Started (essential onboarding step), only retitled |

### Pack

| Current title | Proposed title | Reason |
|---|---|---|
| "Create a Multi-Component Product" | **"Compose a Multi-Component Product"** | "Compose" matches the page's own description and names the action the reader performs. This surviving tutorial covers a full 3-level hierarchy (platform → products → services) plus env vars |
| "Add Resources from HTTP URLs" + "Working with HTTP Resources" | **"Add HTTP Resources"** | The how-to recipe and the tutorial deep-dive are merged into one guide: steps first, then reference sections (media type, auth methods, non-GET, tuning, v1 migration, troubleshooting). "Wget" dropped from the title since the type will be deprecated; `Wget/v1` API identifiers stay in the body |
| "Working with OCI" (pack half) | **"Add OCI Artifacts"** | "Add" matches the sibling pack titles ("Add Resources from GitHub", "Add HTTP Resources"); names the concrete pack action. Transfer payoff moves to the transfer half |
| "Working with Resolvers" (pack half) | **"Add Component References"** | The pack half runs the authoring narrative: declare `componentReferences` in a constructor and push. The source only shows references to a separate repository, but a reference needs no separate repo, so the guide is broadened to open with the general same-repo case and then show the cross-repo case. With both covered, "External" would be an inaccurate scope word and is dropped, giving an exact noun-phrase match with the transfer half "Resolve Component References" (add the references here, resolve them there). "Add" matches the sibling pack titles |
| "Working with Git Repositories" | **"Add Git Repositories"** | "Add" matches the sibling pack titles ("Add Resources from GitHub", "Add OCI Artifacts", "Add HTTP Resources"); names the concrete pack action (add a Git repo as a resource, source, or embedded snapshot). Slug renamed `working-with-git.md` → `add-git-repositories.md` so the URL matches the new title and the add-* siblings; old URL aliased |

"Configure a Versioning Scheme" keeps its current title; it is already an
imperative verb phrase.

**Dropped page.** The how-to `model-products.md` ("Model Software Products") is
dropped. It covered the same subject as the tutorial above (composing a
multi-component product from references) but only a shallow 2-level recipe, so it
was redundant with the deeper tutorial. Its old URL `/docs/how-to/model-products/`
redirects to the surviving page via an alias, so no inbound link breaks.

### Sign

| Current title | Proposed title | Reason |
|---|---|---|
| "Configure Credentials for Signing" | **"Configure Signing Credentials"** | Reorders to the {scope} Credentials shape so all three credentials guides match: "Configure Signing Credentials" (sign), "Configure Registry Credentials" (transfer), "Configure Controller Credentials" (deploy) |
| "Plain Signatures" | **"Sign with Plain RSA"** | Noun → verb phrase; algorithm explicit |
| "Certificate Chains (PEM)" | **"Sign with a PEM Certificate Chain"** | Noun → verb phrase |
| "GPG Signatures" | **"Sign with GPG"** | Noun → verb phrase |
| "Sigstore (Keyless)" | **"Sign with Sigstore (Keyless)"** | Verb prefix added |

### Transfer

_Grouped by sub-theme, matching the tree order._

**Move components**

| Current title | Proposed title | Reason |
|---|---|---|
| _(no page on `main`)_ | **"Transfer Component Versions"** (NEW) | Base transfer guide added as the phase entry point. No `main` page teaches the default `ocm transfer cv <source> <target>`; it existed only as concept prose and copy-pasted boilerplate. See Pages Added |
| "Transfer Helm Charts with OCM" | **"Transfer Components with Helm Charts"** | "with OCM" is implied by the section; "Components" restores the component framing of its siblings ("Transfer Components across an Air Gap", "Transfer Components Continuously"). The source transfers a component version that carries a Helm chart resource, not a standalone chart, so "with Helm Charts" scopes the specialization |
| "Replicate Component Versions with the Controller" | **"Transfer Components Continuously"** | Renamed to surface what it is: a repo-to-repo transfer (moved from deploy for that reason), parallel to "Transfer Components across an Air Gap". "Replicate" buried the operation and "with the Controller" named the mechanism, not the value. "Continuously" captures the real differentiator from CLI transfer: the Replication controller re-runs the transfer on each new source version. The controller mechanism is explained in the body; the slug keeps the `Replication` CRD name |

(Tree order of this group: **Transfer Component Versions** → **Transfer Components across an Air Gap** → **Transfer Components with Helm Charts** → **Transfer Components Continuously** — the base guide first, then the whole-component transfer specializations. "Transfer Components across an Air Gap" has no row here because it keeps its title unchanged, already an imperative verb phrase. "Upload OCI Images" is a specialization too, but it configures an uploader rather than running a whole-component transfer, so it sits with the other upload guides, see "Configure uploads" below.)

**Retrieve artifacts**

| Current title | Proposed title | Reason |
|---|---|---|
| "Download Resources from Component Versions" | **"Download Resources"** | "from Component Versions" is redundant in context |
| "Working with SBOMs" (transfer half) | **"Download SBOMs"** | Gerund → imperative "Download …" verb, paired with "Download Resources"; "from Component Versions" dropped |
| "Working with OCI" (transfer half) | **"Pull OCI Artifacts Natively"** | The payoff half: once transferred, the embedded artifacts are pullable straight from the registry with standard OCI tooling (`docker pull`, `oras pull`, `crane`), no OCM CLI. "Pull" and "Natively" mark the OCI-index native-access concept and distinguish it from "Download Resources" (OCM-mediated download to a local file). "after transfer" is implied by the transfer section |

**Resolve references**

| Current title | Proposed title | Reason |
|---|---|---|
| "Working with Resolvers" (transfer half) + "Resolving Components across Multiple Registries" | **"Resolve Component References"** | The two pages taught the same task at different fidelity: the tutorial's transfer half walks a single-repo graph, the how-to is the multi-repo variant (it even ended by pointing back at the tutorial). Merged into one guide titled for the goal (resolve a reference graph), not the mechanism ("Resolvers"). The multi-repo how-to's enduring bits (glob patterns, `versionConstraint` for version-split repos, specificity ordering) are not procedure, so they lift into the Resolvers concept page (see below); the merged guide keeps the hands-on walkthrough and the realistic separate-repositories case |

**Manage credentials**

| Current title | Proposed title | Reason |
|---|---|---|
| "Configure Credentials for Multiple Registries" | **"Configure Registry Credentials"** | Names the concept object: credentials scoped to OCI registries (the `OCIRegistry` consumer-identity type). "Multiple" was the scenario, not the concept, the body still teaches explicit per-registry pins plus a Docker-config catch-all. Mirrors the deploy sibling "Configure Controller Credentials" ({scope} Credentials shape). "Understand Credential Resolution" is extracted into Concepts (see Documents That Move Between Sections). The configure → migrate pairing is preserved, but the migrate page now sits in the trailing "Migrate legacy configuration" group (see below). Slug renamed `configure-multiple-credentials.md` → `configure-registry-credentials.md` so the URL matches the new title and the sibling `configure-signing-credentials` / `configure-controller-credentials` slugs; old URL aliased (see Aliases) |

**Tune networking**

| Current title | Proposed title | Reason |
|---|---|---|
| "Configure HTTP Behaviour" (6 merged pages) | **"Configure HTTP Behaviour"** | Keeps the source `_index` title; the merged page covers timeouts, retry, TLS, custom CA, and proxy, i.e. HTTP behaviour knobs, not a client object. Sits next to "Configure Registry Credentials": both configure *how the client connects* to a registry (auth and transport), ahead of the pages that configure *what gets uploaded*. **Scope trimmed to task recipes:** the five source pages inline a lot of field-level documentation (the full `http.config.ocm.software/v1alpha1` schema, the per-field defaults table, Go `time.ParseDuration` syntax, per-host merge rules) that already lives in `reference/http-client-configuration.md`. The merged guide keeps only the task walkthroughs (route through a proxy, trust a private CA, bound timeouts for a slow link, per-host overrides) and links down to that reference for the schema and defaults instead of restating them, mirroring the uploader guides' link-down-to-reference pattern |

**Configure uploads**

_All four pages in this group use the imperative verb **Upload** and are co-located because each configures an uploader that writes resources into the target during transfer. Order runs from the built-in OCI uploader, to the generic custom-target uploader, to the two vendor-registry specializations._

| Current title | Proposed title | Reason |
|---|---|---|
| _(no page on `main`)_ | **"Upload OCI Images"** (NEW) | Added to answer the common "how do I push my component's regular OCI images so they are directly pullable?" question via the built-in OCI uploader. Leads the uploads group as the simplest, no-config case (the default `match` already selects OCI image resources). Named **"Upload …"**, not "Publish …", to keep one verb for the write-to-target operation across the phase, matching "**Upload** to a Custom Target", "**Upload** to JFrog Artifactory", and "**Upload** to Sonatype Nexus". See Pages Added |
| "Configure Custom Uploads During Transfer" | **"Upload to a Custom Target"** | Retitled from "Configure Custom Uploads" to the group's **Upload to \<target\>** shape (parallel to the JFrog and Nexus siblings): the page teaches the built-in HTTP streaming uploader that routes a matching resource to an arbitrary custom `PUT` endpoint, so it is the *generic* member of the vendor-specific uploads, not a config-knob page like "Configure Registry Credentials" / "Configure HTTP Behaviour". "During Transfer" is implied by the phase. Slug changes `configure-custom-uploads.md` → `upload-to-custom-target.md`, so the old URL gets an alias (see Aliases) |
| "JFrog Artifactory" + 4 format pages (helm-charts, maven-artifacts, npm-packages, generic-files) | **"Upload to JFrog Artifactory"** | The vendor `_index.md` and its four format pages collapse into one flat guide with a per-format section. Imperative, names the action. The shared scaffold (credentials, transfer, verify, troubleshooting) is written once; per-format sections keep only the `match`/`path` snippet and consumer command. Repository-behaviour facts move to `reference/.../artifactory-uploader.md`; the uploader model moves to `concepts/vendor-uploaders.md` |
| "Sonatype Nexus" + 4 format pages (helm-charts, maven-artifacts, npm-packages, raw-files) | **"Upload to Sonatype Nexus"** | Same collapse as the Artifactory guide. Repository-behaviour facts (never-overwrite semantics, Maven snapshot routing, components-API npm upload) move to `reference/.../nexus-uploader.md` |
| "Using Vendor-Specific APIs" (top overview) | **dropped** | The `how-to/vendor-specific-apis/_index.md` overview is removed: its "How vendor uploaders work" section becomes `concepts/vendor-uploaders.md`, its product list is covered by the two guides above. Old URL redirects to the new concept (see aliases) |

**Migrate legacy configuration**

_All one-time migration guides are grouped at the end of the transfer section, after the live-workflow pages, since they are run once to move off deprecated v1 configuration rather than as part of the recurring transfer workflow. Relative order follows their topics above (resolvers → credentials → uploads)._

| Current title | Proposed title | Reason |
|---|---|---|
| "Migrate from Fallback to Deterministic Repository Resolvers" | **"Migrate Legacy Resolvers"** | Shortened; drops "Deterministic", which only makes sense as a contrast to the deprecated fallback resolver. In v2 there is a single resolver type (glob-based `resolvers.config.ocm.software/v1alpha1`), so the title names the legacy fallback config being migrated away from, exactly parallel to "Migrate Legacy Credentials". Slug renamed `migrate-from-deprecated-resolvers.md` → `migrate-legacy-resolvers.md` to match the new title and the sibling `migrate-legacy-credentials.md`; old URL aliased |
| "Migrate Legacy Credentials" | (unchanged) | Already imperative/consistent |
| "Migrate from --upload-as to Uploader Configurations" | **"Migrate --upload-as Flags"** | Shortened; drops the "to Uploader Configurations" tail (names the target mechanism, which the body explains). Keeps the recognizable deprecated flag so readers who use `--upload-as` find it. Full slug kept as alias |

### Deploy

| Current title | Proposed title | Reason |
|---|---|---|
| "Configure Credentials for OCM Controllers" | **"Configure Controller Credentials"** | Shorter; "OCM" implied by section |
| "Verify Component Versions in the Controller" | **"Verify Component Signatures on Deployment"** | "Signatures" is what is actually verified (more precise than "Versions"); "on Deployment" replaces "in the Controller" to keep the deploy-time context and distinguish it from the sign-phase "Verify Component Versions" (CLI verification) |
| "Deploy Manifests with Deployer" | **"Deploy Kubernetes Manifests with the OCM Deployer"** | Matches the source wording; "manifest" names the artifact stored in the component (like the sibling titles name their artifact: RGDs, Helm chart); "with the OCM Deployer" names the mechanism (applied directly, no kro/Helm/GitOps packaging layer) |
| "Configure Custom RBAC for Deployers" | **"Configure Custom RBAC"** | "for Deployers" dropped; the deploy phase implies the deployer context. Slug renamed `custom-rbac.md` → `configure-custom-rbac.md` to match the new title and the sibling `configure-*` guides; old URL aliased |
| "Deploy an Application from a Helm Chart with OCM and kro" | **"Deploy a Helm Chart with kro and GitOps"** | Surfaces the delivery stack (kro plus a GitOps deployer, Flux or Argo CD); the long "Application from … with OCM" subtitle moves to the page description |
| "Deploy an Application from Chained RGDs with OCM and kro" | **"Deploy with Chained kro RGDs"** | Names the mechanism (chained kro RGDs, deployed as plain manifests, no Helm or GitOps); RGD = ResourceGraphDefinition |

Note: "Add and Verify Ownership Information" keeps its title and stays whole in
pack. It is already an imperative verb phrase, and the two-command `oras
discover` readback is too small to warrant a split. "Discover Component Graphs"
also keeps its title (already an imperative verb phrase); it moves into deploy,
see the phase-assignment table below.

---

## Documents That Move Between Sections

### From How-to → Getting Started

| Document | Proposed location | Reason |
|---|---|---|
| `how-to/container-image-usage.md` | `getting-started/run-cli-from-container-image.md` | Running the CLI from a container image is phase-agnostic (every phase invokes `ocm`), not a pack-authoring task. Belongs beside `ocm-cli-installation.md` as the second way to get a working CLI, retitled to pair with it |

No pages move out of Getting Started. It retains `ocm-cli-installation.md`,
`create-component-version.md`, `setup-controller-environment.md`, and
`deploy-helm-chart.md`, and gains the container-image CLI guide moved in from
How-to. Two of these pages are retitled in place (see Title Changes).

### From Deploy → Transfer (within Guides)

| Document | Proposed location | Reason |
|---|---|---|
| `how-to/replicate-component-versions-controller.md` | `guides/transfer/replicate-component-versions.md` | "Transfer Components Continuously" copies a component version between two OCM repositories (repo-to-repo), which is a transfer operation. It sat under deploy only because the Replication controller drives it, but the controller is the mechanism, not the category. Moving it to the transfer phase matches its semantics; the title now says transfer and "Continuously" marks the controller's auto re-run on each new source version |

### From Tutorials → Concepts

| Document | Proposed location | Reason |
|---|---|---|
| `tutorials/credential-resolution.md` ("Understand Credential Resolution") | `concepts/credential-resolution.md` ("Credential Resolution") | The page runs no procedure. It is a mental model of the matching algorithm: how a lookup identity is built, the three chained matchers (path glob, URL scheme/port, equality), first-match-wins, port and `oci`-scheme defaults, five worked examples, and a short 401 troubleshooting pair. None of it is transfer-specific; credential resolution applies to every phase. It belongs in Concepts beside `credential-system.md`, which it already links to as "the full concept". `credential-system.md` stays the *why/what* overview (consumers, repositories-as-fallback, identity/credential separation); the new page is the layered *how the matcher decides* detail. Extracting it simplifies the transfer credentials cluster to two how-tos (configure → migrate) and gives the "why did the wrong credential get picked?" reader a single Concepts home. "Configure Credentials for Multiple Registries" cross-links down to it |

### From How-to → Concepts

| Document | Proposed location | Reason |
|---|---|---|
| `how-to/vendor-specific-apis/jfrog-artifactory/_index.md` + `.../sonatype-nexus/_index.md` ("How vendor uploaders work" sections) | `concepts/vendor-uploaders.md` ("Vendor Repository Uploaders") | Both vendor overviews repeat the same server-side model: an uploader rule selects resources by access type (first-match-wins across rules), the uploader reads the repository type/format from the server at upload time, uploads the way that type expects, rewrites the resource access to `Helm/v1` (charts) or `Wget/v1` (files/packages), verifies a `genericBlobDigest/v1` digest, and only local/hosted repositories accept uploads. None of this is a procedure, so it becomes a single concept page beside `transfer-concept.md`. The two vendor guides and the reference uploader pages link to it instead of re-explaining it |

### Extended Concept: Resolvers

Unlike the credential-resolution move, no page relocates here. The resolver
*task* survives as the merged "Resolve Component References" guide; what moves is
the enduring *mechanics* that the two resolver how-tos carried inline. These are
lifted into the existing `concepts/resolvers.md`, which already sketches them:

| Lifted from | Into `concepts/resolvers.md` | Reason |
|---|---|---|
| multi-repo how-to "Tips" (glob patterns, specificity ordering) + migrate how-to "Key Differences" / version-split section | the "Configuration" and a new "Matching and Ordering" section | Glob syntax, first-match ordering, specificity, and `versionConstraint` are the resolver *model*, not steps. The concept page already states first-match-wins and glob matching; it absorbs the worked specifics so the guides stop re-teaching them and link here instead |

The two guides keep their procedures and link to the concept for the "how
matching decides" details, mirroring how the credential how-tos link to
`concepts/credential-resolution.md`.

**Install / run pairing.** "Install the OCM CLI" and "Run the OCM CLI from a
Container Image" are deliberately named as a pair and placed adjacent so readers
see them as two ways to get a working `ocm`: install a binary, or run from a
container. This proposal only retitles and reorders them; it does not merge
their bodies. A true merge (one page with a "Run from a container" tab or
section) is a sensible follow-up, but it is a content rewrite rather than a
navigation change: the container page is currently a full create/get walkthrough
that overlaps `create-component-version.md`, so merging it means trimming that
walkthrough down to just the "how to invoke `ocm` from the image" part. Left for
a later content pass.

### Phase assignment inside Guides

Every other how-to and tutorial moves into a lifecycle phase. The non-obvious
assignments:

| Document | Current path | Phase | Reason |
|---|---|---|---|
| `how-to/download-resources-from-component-versions.md` | How-to (flat) | transfer | Download is consumption from an existing component, not authoring |
| `how-to/discover-component-graphs.md` | How-to (flat) | **deploy** | Driven by the `Discovery` controller: needs a controller environment and a `Ready` `Component`, and publishes a filtered graph view into Kubernetes `status`. A controller/cluster operation, not a CLI transfer step, so it lands in deploy with the other controller pages |
| `how-to/migrate-from-upload-as.md` | How-to (flat) | transfer | Uploader configuration applied during transfer |
| `tutorials/configure-custom-uploads.md` | Tutorials (flat) | transfer | Routes resources to a custom upload target during transfer |
| `how-to/vendor-specific-apis/**` | How-to (nested) | transfer | Uploading into vendor registries happens during transfer; the 11-page product → format tree collapses into two flat guides (`upload-to-jfrog-artifactory.md`, `upload-to-sonatype-nexus.md`) plus a concept and reference extensions |
| `tutorials/configure-versioning.md` | Tutorials (flat) | pack | Versioning scheme applies when creating component versions |
| `tutorials/working-with-git.md` | Tutorials (flat) | pack | Adds a Git repo as a resource/source to a component version |

The pack halves of the three split documents stay in `guides/pack/`.
`how-to/add-and-verify-ownership.md` stays whole in pack.

---

## Structural Changes

### Directories Flattened

| Current | Proposed | Reason |
|---|---|---|
| `tutorials/working-with-oci/` (directory with single `_index.md`) | two flat files `add-oci-artifacts.md` (pack) + `pull-oci-artifacts-natively.md` (transfer) | No child pages; the split content lands in two flat files |
| `how-to/configure-http/` (6 files: `_index.md` + 5 pages) | single `configure-http.md` | The five pages (proxy, tls, retry, timeouts, per-host) are task walkthroughs (Goal / Steps / Verify) that merge cleanly under `##` headings. Their inlined field-reference material (schema, defaults table, duration-format rules, per-host merge semantics) is **not** carried into the merged guide: it already exists in `reference/http-client-configuration.md`, so the guide defers to that page and keeps only the recipes |

### Pages Added

The transfer phase on `main` jumps straight to specialized transfers (air gap,
Helm, vendor registries) and configuration pages. There is no guide for the
fundamental operation: push a component version and its OCI image resources to a
registry. That procedure exists only as prose in `concepts/transfer-concept.md`
and as copy-pasted boilerplate inside the specialized pages. Two net-new guides
fill the gap and become the entry point the rest of the phase specializes:

| New page | Covers | Content source |
|---|---|---|
| `guides/transfer/transfer-component-versions.md` ("Transfer Component Versions") | The default `ocm transfer cv <source> <target>`: CTF ↔ OCI-registry source/target combinations, metadata-only transfer (default: descriptor copied, resources referenced in place) vs a self-contained copy via a local-blob uploader, plus prerequisites, credentials, and verification | Procedure distilled from `concepts/transfer-concept.md` and the transfer/verify boilerplate currently duplicated across the air-gap and vendor pages. The concept page stays the *why/model*; this guide is the *how* |
| `guides/transfer/upload-oci-images.md` ("Upload OCI Images") | Uploading regular OCI image resources as independently pullable OCI artifacts via the OCI uploader (`oci.uploader.transfer.config.ocm.software/v1alpha1`): the default `match` (OCIImage / Helm / LocalBlob-holding-an-OCI-manifest on OCI targets), `imageReference` templating, and the fact that this replaces the deprecated `--upload-as ociArtifact` | New content, grounded in `reference/transfer-configuration/oci-uploader.md`. Links to that reference for the full schema and to "Pull OCI Artifacts Natively" for the consume direction |

Both are new authoring work, not relocations, so they carry no `aliases:` and
appear in no move/merge/drop table. They are listed here and in the tree so the
phase has a complete, ordered front door.

### Cross-Link Design: Transfer Hub and Spokes

Unlike the split-page handoff (a linear continue-in-the-next-guide chain with a
shared workspace), the transfer guides form a **hub and spokes**. "Transfer
Component Versions" is the hub: it teaches the default `ocm transfer cv` once.
Every other transfer page is a spoke that assumes that baseline and adds one
specialization (upload images as OCI artifacts, cross an air gap, carry Helm
charts, upload into a vendor registry). The links are a one-way fan-out from hub
to spokes with a short back-link from each spoke, not a sequential handoff, so no
prerequisite banner carries workspace state between them.

**Hub routing block (bottom of "Transfer Component Versions").** A "Next steps"
section points at every spoke so the reader lands on the base procedure first and
then picks the specialization they need:

```md
## Next steps

You transferred a component version with the default settings. Depending on where
and how the resources must land, continue with a specialized guide:

- [Upload OCI Images]({{< relref "docs/guides/transfer/upload-oci-images.md" >}})
  makes the component's container images directly pullable with `docker`, `oras`,
  or `crane`.
- [Transfer Components across an Air Gap]({{< relref "docs/guides/transfer/air-gap-transfer.md" >}})
  moves a self-contained copy across a network boundary.
- [Transfer Components with Helm Charts]({{< relref "docs/guides/transfer/transfer-helm-charts.md" >}})
  handles components that carry a Helm chart resource.
- [Upload to JFrog Artifactory]({{< relref "docs/guides/transfer/upload-to-jfrog-artifactory.md" >}})
  or [Upload to Sonatype Nexus]({{< relref "docs/guides/transfer/upload-to-sonatype-nexus.md" >}})
  route resources into a vendor registry's native layout.

For the model behind all of these, see
[Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}}).
```

**Spoke prerequisite link (top of every specialized transfer guide).** A one-line
banner sends readers to the hub for the baseline instead of each spoke
re-teaching `ocm transfer cv`. Example for "Upload OCI Images":

```md
{{< callout context="note" title="Start from the base transfer guide" >}}
This guide builds on
[Transfer Component Versions]({{< relref "docs/guides/transfer/transfer-component-versions.md" >}}).
Read it first for the default `ocm transfer cv` workflow, credentials, and
verification; the steps below only add the OCI uploader configuration.
{{< /callout >}}
```

The same banner (retargeted wording) tops "Upload OCI Images", "Transfer
Components across an Air Gap", "Transfer Components with Helm Charts", and the two
vendor upload guides. "Upload OCI Images" additionally links **down** to
`reference/transfer-configuration/oci-uploader.md` for the full `match` /
`imageReference` schema and **across** to "Pull OCI Artifacts Natively" for the
consume direction, and the two vendor guides link to `concepts/vendor-uploaders.md`
for the server-side uploader model (see Documents That Move Between Sections).

### Pages Merged

| Current | Proposed | Reason |
|---|---|---|
| `how-to/add-resources-from-http-urls.md` (recipe) + `tutorials/wget-http-resources.md` (deep-dive) | single `add-http-resources.md` | Near-duplicate HTTP pages; the "Guides" merge removes the how-to/tutorial split that justified two pages. Steps first, then reference sections (media type, auth methods, non-GET, tuning, v1 migration, troubleshooting) |
| `how-to/vendor-specific-apis/jfrog-artifactory/_index.md` + its 4 format pages | single `guides/transfer/upload-to-jfrog-artifactory.md` | Shared scaffold (credentials, transfer, verify, troubleshooting) written once; one `##` section per format (Helm, Maven, npm, generic) keeps only the per-format `match`/`path` snippet and consumer command. Repository-behaviour facts move to `reference/.../artifactory-uploader.md`; the uploader model to `concepts/vendor-uploaders.md` |
| `how-to/vendor-specific-apis/sonatype-nexus/_index.md` + its 4 format pages | single `guides/transfer/upload-to-sonatype-nexus.md` | Same collapse as the Artifactory guide (Helm, Maven, npm, raw sections). Repository-behaviour facts move to `reference/.../nexus-uploader.md` |

### Cross-Link Design: Phase-Entry Prerequisites

Beyond the split handoff (shared-workspace chain) and the transfer hub-and-spokes,
three phases have a single setup step every guide in the phase assumes. Today
each guide re-teaches it. The restructure factors that step out to one page and
has the phase guides link to it as a prerequisite instead of repeating it. The
link is a short banner at the top of each guide, reusing the existing
`{{< callout context="note" >}}` + `{{< relref >}}` convention, no workspace
state is carried.

| Phase | Prerequisite page | Guides that link to it | Repeated setup it removes |
|---|---|---|---|
| pack | `getting-started/create-component-version.md` ("Create Component Versions") | every `guides/pack/*` guide (Add HTTP Resources, Add OCI Artifacts, Add Resources from GitHub, Add SBOMs, Add Git Repositories, Add Component References, Add and Verify Ownership, Compose a Multi-Component Product, Configure a Versioning Scheme) | Creating a workspace and the initial component version / constructor skeleton. Every pack guide opens by building a component version before adding to it; the prerequisite link replaces that boilerplate with one pointer to the Getting Started page that already teaches it |
| sign | `guides/sign/generate-signing-keys.md` + `guides/sign/configure-signing-credentials.md` | `guides/sign/sign-component-version.md` (the signing overview) | Key generation and signing-credential setup. The sign overview already sits third behind these two pages in tree order; the banner makes that ordering an explicit prerequisite rather than mere adjacency, so the overview stops restating key/credential setup |
| deploy | `getting-started/setup-controller-environment.md` ("Set Up Your Controller Environment") | every `guides/deploy/*` guide (Configure Controller Credentials, Configure Custom RBAC, Discover Component Graphs, Deploy with Chained kro RGDs, Deploy a Helm Chart with kro and GitOps, Deploy Kubernetes Manifests with the OCM Deployer, Verify Component Signatures on Deployment) | Bootstrapping a controller/cluster environment. Every deploy guide needs a running controller environment, which is exactly why `setup-controller-environment.md` lives in Getting Started; the prerequisite link replaces each guide's cluster-setup preamble with one pointer |

Example banner (top of a pack guide):

```md
{{< callout context="note" title="Prerequisite: a component version" >}}
This guide assumes you have created a component version. If you have not, follow
[Create Component Versions]({{< relref "docs/getting-started/create-component-version.md" >}})
first, then return here to add resources to it.
{{< /callout >}}
```

Scope note: these three phase-entry links are the high-leverage prerequisites.
Finer intra-phase and cross-phase prerequisites (sign-method pages → the signing
overview, "Verify" → "Sign", the migrate guides → their modern-config targets)
are deferred; they remove less duplication and can be added during authoring.

### Pages Dropped

| Dropped | Reason | Old URL redirects to |
|---|---|---|
| `how-to/model-products.md` ("Model Software Products") | Redundant with the deeper `tutorials/advanced-component-constructor.md`: same subject (compose a multi-component product from references), only a shallow 2-level recipe. The surviving tutorial covers the full 3-level case | `/docs/how-to/model-products/` → `guides/pack/compose-multi-component-product.md` |
| `how-to/vendor-specific-apis/_index.md` ("Using Vendor-Specific APIs") | Top overview of the vendor subtree. Its "How vendor uploaders work" section becomes `concepts/vendor-uploaders.md`; its product list is covered by the two new vendor guides. Nothing procedural is lost | `/docs/how-to/vendor-specific-apis/` → `concepts/vendor-uploaders.md` |

### Maximum Nesting Depth

```
docs/guides/<phase>/<page>.md   ← normal deepest level after restructure
```

After this restructure every page is at most `docs/guides/<phase>/<page>.md`, with
no exceptions. Collapsing the vendor-API subtree removed the only two-level
grouping that previously forced an exception.

---

## Aliases Required

Every moved, renamed, or merged page needs `aliases:` entries in its frontmatter
so existing links and bookmarks continue to resolve. GitHub Pages has no
server-side redirects.

On `main` these pages carry **no** `aliases:` of their own, so the only old URL
to preserve per page is its current flat URL. The new canonical URL becomes
`/docs/guides/<phase>/<page>/`, and the alias points the old flat URL at it.

Split pages share one original URL. Hugo generates one redirect page per alias
and the first page it builds that claims a given alias wins, so the **pack half**
(the entry point) is listed as the primary target and the transfer half repeats
the alias for completeness.

Examples:

```yaml
# guides/pack/add-sboms.md   (primary target for the old SBOM URL)
aliases:
  - /docs/tutorials/working-with-sboms/

# guides/transfer/download-sboms.md   (transfer half; repeats for completeness)
aliases:
  - /docs/tutorials/working-with-sboms/

# guides/pack/add-oci-artifacts.md   (primary target for the old OCI URL)
aliases:
  - /docs/tutorials/working-with-oci/

# guides/transfer/pull-oci-artifacts-natively.md   (transfer half)
aliases:
  - /docs/tutorials/working-with-oci/

# guides/pack/add-component-references.md   (primary target for the old resolvers URL)
aliases:
  - /docs/tutorials/configure-resolvers/

# guides/transfer/resolve-component-references.md   (transfer half + merged multi-repo how-to)
aliases:
  - /docs/tutorials/configure-resolvers/                             # transfer-half secondary (pack half holds primary)
  - /docs/how-to/resolve-components-from-multiple-repositories/      # the merged-in how-to's sole URL

# guides/pack/add-and-verify-ownership.md   (whole, not split)
aliases:
  - /docs/how-to/add-and-verify-ownership/

# guides/transfer/configure-http.md   (merge of the configure-http/ directory)
aliases:
  - /docs/how-to/configure-http/
  - /docs/how-to/configure-http/proxy/
  - /docs/how-to/configure-http/tls/
  - /docs/how-to/configure-http/retry/
  - /docs/how-to/configure-http/timeouts/
  - /docs/how-to/configure-http/per-host/

# guides/transfer/download-resources.md
aliases:
  - /docs/how-to/download-resources-from-component-versions/

# guides/transfer/configure-registry-credentials.md   (retitled + slug rename from configure-multiple-credentials.md)
aliases:
  - /docs/how-to/configure-multiple-credentials/

# guides/pack/add-http-resources.md   (merged recipe + deep-dive)
aliases:
  - /docs/how-to/add-resources-from-http-urls/
  - /docs/tutorials/wget-http-resources/

# getting-started/run-cli-from-container-image.md   (moved out of How-to)
aliases:
  - /docs/how-to/container-image-usage/

# guides/pack/compose-multi-component-product.md   (phase move + slug rename; also absorbs the dropped how-to URL)
aliases:
  - /docs/tutorials/advanced-component-constructor/
  - /docs/how-to/model-products/

# concepts/credential-resolution.md   (extracted from the transfer tutorial)
aliases:
  - /docs/tutorials/credential-resolution/

# guides/transfer/upload-to-custom-target.md   (retitled + slug rename from configure-custom-uploads.md)
aliases:
  - /docs/tutorials/configure-custom-uploads/

# guides/transfer/upload-to-jfrog-artifactory.md   (vendor _index + 4 format pages collapsed)
aliases:
  - /docs/how-to/vendor-specific-apis/jfrog-artifactory/
  - /docs/how-to/vendor-specific-apis/jfrog-artifactory/helm-charts/
  - /docs/how-to/vendor-specific-apis/jfrog-artifactory/maven-artifacts/
  - /docs/how-to/vendor-specific-apis/jfrog-artifactory/npm-packages/
  - /docs/how-to/vendor-specific-apis/jfrog-artifactory/generic-files/

# guides/transfer/upload-to-sonatype-nexus.md   (vendor _index + 4 format pages collapsed)
aliases:
  - /docs/how-to/vendor-specific-apis/sonatype-nexus/
  - /docs/how-to/vendor-specific-apis/sonatype-nexus/helm-charts/
  - /docs/how-to/vendor-specific-apis/sonatype-nexus/maven-artifacts/
  - /docs/how-to/vendor-specific-apis/sonatype-nexus/npm-packages/
  - /docs/how-to/vendor-specific-apis/sonatype-nexus/raw-files/

# concepts/vendor-uploaders.md   (extracted from the vendor overview + both _index "How vendor uploaders work" sections)
aliases:
  - /docs/how-to/vendor-specific-apis/

# guides/pack/add-git-repositories.md   (retitled + slug rename from working-with-git.md)
aliases:
  - /docs/tutorials/working-with-git/

# guides/transfer/migrate-legacy-resolvers.md   (retitled + slug rename from migrate-from-deprecated-resolvers.md)
aliases:
  - /docs/how-to/migrate-from-deprecated-resolvers/

# guides/deploy/configure-custom-rbac.md   (retitled + slug rename from custom-rbac.md)
aliases:
  - /docs/how-to/custom-rbac/
```

Every remaining page that only changes phase (for example
`how-to/add-resources-from-github.md` → `guides/pack/add-resources-from-github.md`,
keeping its slug) needs a single alias from its old flat URL
(`/docs/how-to/add-resources-from-github/`) to its new path. Pages that also
change their slug (`advanced-component-constructor` → `compose-multi-component-product`,
`configure-custom-uploads` → `upload-to-custom-target`,
`configure-multiple-credentials` → `configure-registry-credentials`,
`working-with-git` → `add-git-repositories`,
`migrate-from-deprecated-resolvers` → `migrate-legacy-resolvers`, and
`custom-rbac` → `configure-custom-rbac`, each shown with its own alias block
above) alias their old slug URL explicitly. The signing
pages' old URLs include the
space-containing directory, so their aliases are
`/docs/how-to/sign-and-verify/<page>/` (Hugo lowercases and hyphenates "Sign and
Verify") and `/docs/tutorials/signing/<page>/`.

The eight old per-format vendor URLs each had their own page; they now alias to
the top of the merged vendor guide, not to the per-format section anchor. Hugo
aliases a whole page, not a fragment, so a bookmark to
`/docs/how-to/vendor-specific-apis/jfrog-artifactory/maven-artifacts/` lands on
`upload-to-jfrog-artifactory.md` rather than its `#maven-artifacts` section. This
is an accepted granularity loss for the collapse.

Two pages carry pre-existing unrelated aliases on `main`
(`how-to/Sign and Verify/sign-component-version.md` →
`/docs/getting-started/sign-component-versions/`;
`how-to/Sign and Verify/verify-component-version.md` →
`/docs/reference/ocm-cli/verify/componentversions/`). Those are preserved
in addition to the new phase alias.

---

## Guides to Re-Test After the Changes Are Applied

The restructure is a documentation change, so "testing" means running each
affected guide end to end against a live `ocm` CLI (and, for deploy, a controller
cluster) and confirming the commands, outputs, and cross-links still work. Not
every page needs a walkthrough: pure moves and retitles only need link/alias
checks. The pages below **do** change content and MUST be re-tested.

### Tier 1, mandatory command walkthroughs (content changed, highest risk)

These pages are split, merged, or net-new, so the command sequence a reader
follows is different from any single page on `main`. Each MUST be run top to
bottom.

| Guide | Why it must be tested | What to verify |
|---|---|---|
| `guides/pack/add-sboms.md` + `guides/transfer/download-sboms.md` | Split of "Working with SBOMs"; the transfer half is **not** independently runnable | Run the pack half, keep the workspace, follow the handoff callout into the transfer half, confirm the `./transport-archive` CTF and `/tmp/ocm-sbom-tutorial` state carry across and `ocm download resource --sbom` succeeds |
| `guides/pack/add-oci-artifacts.md` + `guides/transfer/pull-oci-artifacts-natively.md` | Split of "Working with OCI" with **two independent tabs** (Embed an OCI Image Layout, Transfer by Value) | Run **both tabs** on both halves; confirm each tab's two "Continues in …" / "continues from" callouts point at the right half and each tab's `/tmp` workspace survives the handoff |
| `guides/pack/add-component-references.md` + `guides/transfer/resolve-component-references.md` | Split of "Working with Resolvers"; the transfer half also **merges in the multi-repo how-to** | Run the pack half to the "Do not call `add cv` yet" boundary, cross into the transfer half, confirm the shared `.ocmconfig` resolver block works for both the single-repo and the merged separate-repositories case |
| `guides/transfer/add-http-resources.md` | Merge of the HTTP recipe how-to + the Wget deep-dive tutorial | Run the steps, then confirm the folded-in reference sections (media type, auth methods, non-GET, tuning, v1 migration, troubleshooting) are still correct |
| `guides/transfer/configure-http.md` | Merge of the 6-file `configure-http/` tree into one page that now **defers field reference** to `reference/http-client-configuration.md` | Run each recipe (proxy, TLS, retry, timeouts, per-host); confirm the deferral link resolves and no schema/defaults content was lost |
| `guides/transfer/upload-to-jfrog-artifactory.md` | Collapse of the 5-page JFrog vendor subtree into one guide with one section per format | Run the shared scaffold once, then each format section (Helm, Maven, npm, generic); confirm the per-format `match`/`path` snippet and consumer command still upload and resolve |
| `guides/transfer/upload-to-sonatype-nexus.md` | Collapse of the 5-page Nexus vendor subtree | Same as JFrog across Helm, Maven, npm, raw; confirm never-overwrite semantics and Maven snapshot routing behave as documented |
| `guides/transfer/transfer-component-versions.md` | **Net-new** base transfer guide and hub for the transfer phase | Run the default `ocm transfer cv` for the documented CTF ↔ OCI-registry combinations and both metadata-only and self-contained copy modes; confirm the "Next steps" hub links all resolve |
| `guides/transfer/upload-oci-images.md` | **Net-new** OCI uploader guide | Run the OCI uploader config, confirm images are independently pullable with `docker`/`oras`/`crane`, and that the down-link to `reference/transfer-configuration/oci-uploader.md` and the across-link to "Pull OCI Artifacts Natively" resolve |

### Tier 2, spot-check for extended content (page kept, new material added)

These pages are not split or merged, but gained lifted-in content, so only the
added sections need checking, not a full rerun.

| Page | Added content to verify |
|---|---|
| `concepts/resolvers.md` | The new "Matching and Ordering" material (glob syntax, first-match ordering, specificity, `versionConstraint`) lifted from the two resolver how-tos reads correctly and the guides now link here instead of re-teaching it |
| `reference/transfer-configuration/artifactory-uploader.md` | Per-format repository-behaviour facts moved in from the collapsed JFrog guide match the existing per-type tables |
| `reference/transfer-configuration/nexus-uploader.md` | Per-format facts (never-overwrite, Maven snapshot routing, components-API npm upload) moved in from the collapsed Nexus guide are correct |
| `concepts/credential-resolution.md`, `concepts/vendor-uploaders.md` | New concept pages read coherently and their inbound links from the guides resolve (no commands to run, prose/model check only) |

### Tier 3, deploy-phase guides (need a controller cluster)

Every `guides/deploy/*` guide gains a prerequisite banner linking to
`getting-started/setup-controller-environment.md` instead of repeating cluster
setup, and "Transfer Components Continuously"
(`guides/transfer/replicate-component-versions.md`) moved out of deploy. Against a
running controller environment, confirm each deploy guide still works from the
banner's assumed starting state, and that the moved continuous-transfer guide
runs in its new transfer location.

### Non-functional checks (all pages)

- **Links and aliases:** every `{{< relref >}}` resolves and every old URL in the
  Aliases Required section redirects to its new page (primary alias for split
  pack halves, secondary for transfer halves).
- **Cross-link banners:** split-handoff callouts, the transfer hub "Next steps"
  block, the spoke prerequisite banners, and the phase-entry prerequisite banners
  all point at existing targets.
- **Nav rendering:** the five new `_index.md` landing pages render the section and
  phase headers correctly.

---

## Summary of Changes by Type

| Change type | Count |
|---|---|
| Section renamed (How-to + Tutorials → Guides) | 1 |
| New `_index.md` landing pages | 5 (guides + pack, sign, transfer, deploy) |
| Documents split across phases (cross-link handoff) | 3 (SBOMs, OCI, Resolvers) → 6 new pages (the Resolvers transfer half also absorbs the multi-repo how-to, see Pages merged) |
| Documents kept whole despite cross-phase commands | 1 (Add and Verify Ownership, stays pack) |
| Documents moved between sections | 4 (How-to → Getting Started: container-image CLI; Tutorials → Concepts: credential resolution; How-to → Concepts: vendor-uploader model; deploy → transfer within Guides: continuous controller transfer) |
| Pages merged | 5 (HTTP recipe + deep-dive → 1; configure-http 6 → 1; resolver tutorial transfer half + multi-repo how-to → "Resolve Component References"; JFrog vendor _index + 4 format pages → 1; Nexus vendor _index + 4 format pages → 1) |
| Pages dropped | 2 (Model Software Products, redundant with the multi-component tutorial; vendor-API overview `_index.md`, its model folds into the new concept) |
| Pages added (new content) | 2 (Transfer Component Versions, the base transfer guide; Upload OCI Images, the OCI uploader guide. Both fill the missing core-transfer front door, see Pages Added) |
| Directories flattened | 3 (working-with-oci/ → 2 flat files; configure-http/ → 1 file; vendor-specific-apis/ 11-page product→format tree → 2 flat guides) |
| Directories kept nested as an exception | 0 (the vendor-API subtree, previously the only exception, is collapsed; max depth is now `guides/<phase>/<page>.md` everywhere) |
| Title changes | 30 |
| Sections unchanged (structure) | Overview, Getting Started (partial), Reference (structure only; two uploader pages gain content, see below) |
| Sections lightly extended | Concepts: gains `credential-resolution.md` (extracted from the old transfer tutorial) and `vendor-uploaders.md` (extracted from the two vendor overviews), and extends `resolvers.md` with the resolver matching mechanics (lifted from the two resolver how-tos). Reference: `artifactory-uploader.md` and `nexus-uploader.md` gain the per-format repository-behaviour facts lifted from the collapsed guides |
