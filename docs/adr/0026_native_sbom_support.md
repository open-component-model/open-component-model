# Native SBOM Support

* **Status**: proposed
* **Deciders**: OCM Technical Steering Committee
* **Date**: 2026-07-22

Technical Story: OCM needs a first-class way to attach Software Bills of Materials
(SBOMs) to resources and to produce a single, tool-consumable SBOM for an entire
component version so that component contents can be audited and scanned for
vulnerabilities.

## Context and Problem Statement

Consumers of an OCM component need to answer "what is inside this component, and is
any of it vulnerable?". SBOMs answer that question, but OCM had no native way to
attach an SBOM to a resource, to discover one already published alongside an OCI
image, or to aggregate the SBOMs of all resources (and referenced child component
versions) into one auditable document.

The solution must:

* attach SBOMs to resources in a signable, reproducible way;
* reuse SBOMs that already exist (attached to OCI images or supplied as files)
  rather than generating them;
* produce an aggregate SBOM that standard tooling (e.g. Trivy, Dependency-Track)
  can scan and audit.

## Decision Drivers

* **Discover, do not generate** — no dependency on scanners such as syft/anchore.
* **Reproducibility & signing** — the SBOM that ships must be fixed at build time
  and covered by the component signature.
* **Tool compatibility** — the aggregate SBOM must be scannable by common SBOM
  tools without special handling.
* **Minimal blast radius** — avoid changing the constructor core contract or
  leaking heavy SBOM dependencies across all bindings.

## Considered Options

* **Option 1 — Build-time discovery + baked local blob + flat aggregate BOM**
  (chosen): a `SBoM/v1` input discovers and bakes an SBOM as a local resource at
  construction; `ocm download sbom` aggregates baked SBOMs into a flat CycloneDX
  document whose hierarchy is carried by the dependency graph.
* **Option 2 — Read-time discovery only**: no baking; `ocm download sbom`
  discovers SBOMs live from the registry on each invocation.
* **Option 3 — Nested aggregate BOM**: aggregate SBOM nests each source SBOM's
  packages inside `component.components` sub-trees (the shape of the OCM v1
  prototype).

## Decision Outcome

Chosen [Option 1](#option-1): build-time discovery baked as a local blob, plus a
flat aggregate CycloneDX document.

Justification:

* SBOMs are discovered once, at construction, and baked as `type: sbom` local
  resources — reproducible, offline-readable, and covered by signing.
* The aggregate document is a **flat** CycloneDX BOM, which vulnerability scanners
  actually consume (nested structures are not scanned; see Option 3 cons).
* No change to the constructor's `ResourceInputMethod` contract: subject
  references are resolved by a pre-construction pass over the parsed constructor.
* Heavy SBOM-assembly dependencies are isolated in dedicated binding modules.

### Option 1

#### Description

Two attachment models are supported:

* **Label-linked** — a resource of `type: sbom` is linked to the resource it
  describes via a `ocm.software/sbom` label whose value lists the described
  resource identities.
* **On-image** — an SBOM attached to a resource's OCI image, discovered from
  buildx in-index attestations (in-toto SPDX/CycloneDX predicates) or the OCI
  Referrers API.

A new **`SBoM/v1` resource input** performs on-image discovery at construction
time and bakes the result:

* It references its subject resource **by name** (`resource: { name: <res> }`).
  A **pre-construction pass** in the CLI resolves that name to the subject's
  access and embeds it in the input spec before construction runs. This is
  possible because a resource's access is static input data known at parse time,
  so no change to the input-method contract or construction ordering is required.
* For multi-architecture images it selects **exactly one** SBOM via a required
  `platform` attribute (e.g. `linux/amd64`); a multi-arch image without a platform
  selector is an error.
* The discovered SBOM is baked **verbatim, in its original format** (e.g. SPDX
  stays SPDX). No conversion occurs at build time.
* The input **auto-adds** the `ocm.software/sbom` back-link label pointing at the
  subject, so the baked SBOM is discoverable.

The **`ocm download sbom <cv> [--recursive]`** command assembles an orchestrating
SBOM:

* It is **baked-only**: it reads `type: sbom` local blobs discovered via the
  `ocm.software/sbom` label and performs no live registry discovery.
* It emits a **single CycloneDX 1.6 document**. SPDX (and CycloneDX XML) inputs are
  normalized to CycloneDX JSON. Format detection is **content-authoritative**: the
  document body decides the format; the media type is only a fallback hint.
* With `--recursive` it descends into referenced child component versions, cycle
  guarded by a visited set.

#### High-level Architecture

Construction (bake):

```text
constructor.yaml
  resource podinfo      (access: OCIImage/v1)
  resource podinfo-sbom (input: SBoM/v1, resource:{name: podinfo}, platform: linux/amd64)
        │
        ▼  pre-construction pass: copy podinfo.access into the SBoM/v1 input
        ▼  SBoM/v1 input method: FetchImageSBOMs → select platform → bake verbatim
  descriptor:
    resource podinfo-sbom
      access: localBlob (application/spdx+json)
      label:  ocm.software/sbom → { references: [ {resource: {name: podinfo}} ] }
```

Download (aggregate):

```text
ocm download sbom <cv> --recursive
  for each resource: FindSBOMResources (label) → read baked local blob → normalize→CDX
  descend referenced CVs (recursive)
  assemble → one flat CycloneDX 1.6 BOM
```

Aggregate BOM shape — **flat components, hierarchy in the dependency graph**:

```text
components: [ <all CV/resource wrappers AND all packages, flat, no nesting> ]
dependencies:
  <root CV>            dependsOn [ <root resources...>, <child CV> ]
  <child CV>           dependsOn [ <child resources...> ]
  <resource>           dependsOn [ <its packages...> ]
  <package>            dependsOn [ <its package deps...> ]
```

bom-refs are namespaced `component@version:resource:name[:originalRef]` (with a
`#N` suffix if a resource yields multiple SBOMs) to prevent collisions.

#### Contract

* **Label** `ocm.software/sbom` (version `v1`), value
  `{ references: [ { resource: <identity> } ] }`, marked signing-relevant.
* **Input** `SBoM/v1`: `{ resource: <identity>, platform?: <os/arch[/variant]> }`;
  `access` is populated by the pre-construction pass, not authored by hand.
* **Resource type** `sbom`; baked access is `localBlob` with the SBOM's original
  media type.
* **Discovery** (bindings): `descriptor.FindSBOMResources` (label model);
  `oci.Repository.FetchImageSBOMs` returning `oci.SBOM{ Blob, MediaType, Format,
  Platform, … }` (on-image model). The `oci.ImageSBOMDownloader` interface is the
  shared contract between the CLI and the input method.
* **Aggregate output**: CycloneDX 1.6 JSON; SPDX/CycloneDX-XML inputs normalized to
  CycloneDX; flat `components` + `dependencies` hierarchy.

## Pros and Cons of the Options

### [Option 1] Build-time discovery + baked local blob + flat aggregate BOM

Pros:

* Reproducible and signable: the SBOM is fixed at build time and stored locally.
* `download sbom` needs no network access and no credentials.
* Flat aggregate BOM is scanned correctly by Trivy and other SBOM tools.
* No constructor-core contract change; heavy deps isolated in new modules.

Cons:

* Requires a build-time step to attach SBOMs; components not built with it produce
  empty aggregates (surfaced as warnings).
* The aggregate expresses containment via `dependencies`, a slight semantic
  overload of `dependsOn`.

### [Option 2] Read-time discovery only

Pros:

* No build-time step; works against images already in a registry.

Cons:

* Not reproducible or signable; results depend on live registry state.
* Requires network access and credentials at download/audit time.
* Fails for local/offline transport archives.

### [Option 3] Nested aggregate BOM

Pros:

* Models physical containment directly; visually hierarchical.

Cons:

* **Vulnerability scanners (e.g. Trivy) do not recurse into nested
  `component.components`** — such a BOM scans as empty (0 components), defeating
  the auditing goal.

## Discovery and Distribution

* SBOM-assembly dependencies (`cyclonedx-go`, `protobom`) live in a dedicated
  `bindings/go/sbom` module; the `SBoM/v1` input lives in `bindings/go/input/sbom`
  and does not pull SBOM-conversion libraries. On-image discovery and the shared
  `ImageSBOMDownloader` interface live in `bindings/go/oci`.
* The CLI wires the pre-construction resolution pass into `add component-version`,
  registers the built-in `SBoM/v1` input method, and exposes `download sbom`.
* SPDX→CycloneDX normalization is lossy for fields without a CycloneDX equivalent;
  acceptable for an aggregate overview and documented in command help.

## Conclusion

OCM attaches SBOMs as signed, reproducible local resources discovered at build
time, and aggregates them on demand into a single flat CycloneDX document whose
hierarchy is expressed through the dependency graph. This delivers a
tool-scannable, auditable SBOM for a whole component version — recursively across
referenced component versions — without generating SBOMs, changing the constructor
core, or leaking heavy dependencies across the bindings.
