# Ownership Annotations for OCI Image Resources (Asset-to-Owner)

* **Status**: proposed
* **Deciders**: OCM Technical Steering Committee
* **Date**: 2025.02.06

**Technical Story**: [ocm-project#823](https://github.com/open-component-model/ocm-project/issues/823) — Implement ownership annotations to OCI image resources (asset-to-owner scenario). Parent epic: [ocm-project#457](https://github.com/open-component-model/ocm-project/issues/457).

---

## Context and Problem Statement

OCM stores resources (e.g. container images) as standard OCI artifacts in registries. Once stored, these artifacts look like any other image — there is no way to tell which OCM component version they belong to.

This is the **"asset-to-owner" problem**: given an OCI artifact, find the component version that shipped it. Without this link:

- **Security teams** cannot quickly find who owns a vulnerable image.
- **Auditors** must manually cross-reference images to component versions.
- **Platform teams** have no automated way to build an inventory of deployed components.
- **Air-gapped transfers** lose the connection to the originating component version entirely.

The [OCM OCI spec section 6.3](https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/03-storage-backends/oci.md#63-asset-annotations) defines annotation keys that solve this, but neither codebase implements them yet.

---

## Decision Drivers

* Security and compliance tools need to trace artifacts back to their component versions
* The OCM spec already defines the annotation keys — we should follow it
* Both legacy and new OCM need to be considered
* Annotations must not break digest verification or signatures

---

## Spec Summary

Full specification: [OCM OCI spec section 6.3 — Asset Annotations](https://github.com/open-component-model/ocm-spec/blob/e9273b126045b96e11cc9caf056363728c76bec8/doc/04-extensions/03-storage-backends/oci.md#63-asset-annotations)

Key points:

- OCM implementations **MAY** add annotations to the **top-level** manifest or index of OCI artifacts imported as resources or sources. Annotations are optional and must not affect descriptor selection.
- Three normative keys are defined: `software.ocm.component.name`, `software.ocm.component.version`, and `software.ocm.artifact`. Values must exactly match the Component Descriptor. The artifact annotation is a JSON array of identity objects (JCS-canonicalized).
- **Integrity**: Annotations may be added freely to newly created artifacts. For existing artifacts, modifying them changes the digest — the original digest should be saved in `software.ocm.base.digest`, and re-signing may be needed.
- **Verification**: Verifiers may strip OCM annotations before checking digests/signatures, using the base digest for comparison. This keeps verification stable regardless of annotations.
- The `ociArtifactDigest/v1` normalization algorithm is extended so that stripping annotations produces a digest equal to `software.ocm.base.digest`.

---

## Annotation Inventory

This section lists all OCM annotations across both codebases, marking their current status.

### Annotations Present in Both Codebases

These annotations exist in the legacy OCM ([`genericocireg/componentversion.go`](https://github.com/open-component-model/ocm/blob/main/api/ocm/extensions/repositories/genericocireg/componentversion.go)) **and** the new OCM ([`bindings/go/oci/spec/annotations/`](https://github.com/open-component-model/open-component-model/tree/main/bindings/go/oci/spec/annotations)). They use the `software.ocm.` (dot) prefix:

| Annotation Key | Purpose | Used On | Set In |
|---|---|---|---|
| `software.ocm.componentversion` | Combined `name:version` identifier | Component descriptor manifests/indexes | New: [`store_descriptor.go#L112`](https://github.com/open-component-model/open-component-model/blob/main/bindings/go/oci/store_descriptor.go#L112), Legacy: [`componentversion.go#L286`](https://github.com/open-component-model/ocm/blob/main/api/ocm/extensions/repositories/genericocireg/componentversion.go#L286) |
| `software.ocm.creator` | User agent / creator identifier | Component descriptor manifests/indexes | New: [`store_descriptor.go#L113`](https://github.com/open-component-model/open-component-model/blob/main/bindings/go/oci/store_descriptor.go#L113), Legacy: [`componentversion.go#L287`](https://github.com/open-component-model/ocm/blob/main/api/ocm/extensions/repositories/genericocireg/componentversion.go#L287) |
| `software.ocm.artifact` | Artifact identity + kind (resource/source) | Layer descriptors within component descriptor manifests | New: [`descriptor.go#L78-L81`](https://github.com/open-component-model/open-component-model/blob/main/bindings/go/oci/internal/identity/descriptor.go#L78-L81), Legacy: [`componentversion.go#L297`](https://github.com/open-component-model/ocm/blob/main/api/ocm/extensions/repositories/genericocireg/componentversion.go#L297) |

### Annotations Present Only in Legacy OCM

These annotations use the `software.ocm/` (slash) prefix and are specific to the legacy OCM's artifact set format:

| Annotation Key | Purpose | Used On | Set In |
|---|---|---|---|
| `software.ocm/component-version` | Combined `name:version` identifier | OCI artifact manifests (docker imports) | [`dockerdaemon/access.go#L67`](https://github.com/open-component-model/ocm/blob/main/api/utils/blobaccess/dockerdaemon/access.go#L67), [`dockermulti/access.go#L68`](https://github.com/open-component-model/ocm/blob/main/api/utils/blobaccess/dockermulti/access.go#L68) |
| `software.ocm/main` | Main artifact digest in an artifact set | Artifact set indexes | [`utils_synthesis.go#L112`](https://github.com/open-component-model/ocm/blob/main/api/oci/extensions/repositories/artifactset/utils_synthesis.go#L112), [`artifactset.go#L104`](https://github.com/open-component-model/ocm/blob/main/api/oci/extensions/repositories/artifactset/artifactset.go#L104) |
| `software.ocm/tags` | Tags assigned to a manifest | Artifact set manifests | [`artifactset.go#L219`](https://github.com/open-component-model/ocm/blob/main/api/oci/extensions/repositories/artifactset/artifactset.go#L219) |
| `software.ocm/type` | Artifact type | (Defined but unused) | [`annotations.go#L13`](https://github.com/open-component-model/ocm/blob/main/api/oci/annotations/annotations.go#L13) (defined only) |

### Proposed Ownership Annotations (New)

These annotations are defined in the [OCM OCI spec section 6.3](https://github.com/open-component-model/ocm-spec/blob/e9273b126045b96e11cc9caf056363728c76bec8/doc/04-extensions/03-storage-backends/oci.md#63-asset-annotations) but **do not exist in either codebase yet**. They target resource OCI manifests/indexes (not descriptor manifests):

| Annotation Key | Purpose | Value Format | Set In |
|---|---|---|---|
| `software.ocm.component.name` | Component name | Plain string | Not yet implemented |
| `software.ocm.component.version` | Component version | Plain string | Not yet implemented |


**Naming rationale**: All new annotations use `software.ocm.` with dot separators, matching the pattern established in both codebases (`software.ocm.componentversion`, `software.ocm.creator`). The spec uses separate keys (`.component.name`, `.component.version`) rather than the combined `software.ocm.componentversion` format because separate keys are clearer for resource artifact annotations and easier for tools to query.

### Example: Annotated OCI Manifest

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "config": { "...": "..." },
  "layers": [ "..." ],
  "annotations": {
    "software.ocm.component.name": "github.com/acme/myapp",
    "software.ocm.component.version": "1.2.3",
    "software.ocm.artifact": "[{\"identity\":{\"name\":\"backend\",\"version\":\"1.2.3\"},\"kind\":\"resource\"}]",
    "software.ocm.component.provider": "acme.org",
    "software.ocm.component.repository": "ghcr.io/acme/ocm"
  }
}
```

---

## Proposed Changes

### New OCM (`https://github.com/open-component-model/open-component-model`)

The new OCM already writes `software.ocm.artifact` when packing resources into OCI artifacts. The main change is to also write `software.ocm.component.name` and `software.ocm.component.version` at the same time.

**What to do:**

1. **Define new annotation constants** in [`bindings/go/oci/spec/annotations/annotations.go`](https://github.com/open-component-model/open-component-model/blob/main/bindings/go/oci/spec/annotations/annotations.go) — add keys for component name, component version, and base digest.

2. **Pass component name and version into the pack pipeline** — the entry point (`uploadAndUpdateLocalArtifact` in [`bindings/go/oci/repository.go`](https://github.com/open-component-model/open-component-model/blob/main/bindings/go/oci/repository.go)) already knows the component name and version. Add these as fields on `pack.Options` so they flow down to where annotations are written.

3. **Write the annotations during packing** — the function `identity.Adopt()` in [`bindings/go/oci/internal/identity/descriptor.go`](https://github.com/open-component-model/open-component-model/blob/main/bindings/go/oci/internal/identity/descriptor.go) already writes the `software.ocm.artifact` annotation. Extend it to also write the two new ownership keys. This covers both code paths: OCI Layout resources and plain blob resources.

4. **Existing OCI images (deferred)** — when an OCI image is uploaded via `uploadOCIImage()`, it is copied as-is without annotations. Adding annotations here would change the digest, which requires `software.ocm.base.digest` handling and possibly re-signing. This is **out of scope for the initial implementation** (Option 1) but can be added later (Option 2).

---

### Legacy OCM (`https://github.com/open-component-model/ocm`)

The legacy OCM already sets `software.ocm/component-version` when importing docker images, and defines `software.ocm.componentversion`, `software.ocm.creator`, and `software.ocm.artifact` in [`genericocireg/componentversion.go`](https://github.com/open-component-model/ocm/blob/main/api/ocm/extensions/repositories/genericocireg/componentversion.go). However, it does not set the spec's separate-key ownership annotations (`software.ocm.component.name`, `.version`).

**What to do:**

1. **Define new annotation constants** in [`api/oci/annotations/annotations.go`](https://github.com/open-component-model/ocm/blob/main/api/oci/annotations/annotations.go) — same keys as the new OCM (`software.ocm.component.name`, `software.ocm.component.version`, `software.ocm.base.digest`).

2. **Update docker image imports** — in [`api/utils/blobaccess/dockerdaemon/access.go`](https://github.com/open-component-model/ocm/blob/main/api/utils/blobaccess/dockerdaemon/access.go) and [`dockermulti/access.go`](https://github.com/open-component-model/ocm/blob/main/api/utils/blobaccess/dockermulti/access.go), add the new annotation keys alongside the existing `COMPVERS_ANNOTATION`. This is the simplest win: the code already writes one annotation, just add two more.

3. **General resource uploads (optional)** — the blob handler ([`api/ocm/extensions/blobhandler/handlers/oci/ocirepo/blobhandler.go`](https://github.com/open-component-model/ocm/blob/main/api/ocm/extensions/blobhandler/handlers/oci/ocirepo/blobhandler.go)) and OCI transfer functions ([`api/oci/tools/transfer/transfer.go`](https://github.com/open-component-model/ocm/blob/main/api/oci/tools/transfer/transfer.go)) copy artifacts without annotations. Adding annotations here is harder because it changes digests. This can be deferred.

4. **Read/display support** — add the ability to read and show ownership annotations in CLI output (e.g. `ocm describe componentversion`). This is low effort and gives legacy OCM users visibility even if the legacy codebase doesn't write annotations itself.

---

## Where to Document

1. **OCM Website** ([ocm-website](https://github.com/open-component-model/ocm-website))
   - New page under `content/docs/concepts/` explaining ownership annotations and the asset-to-owner use case.
   - Update OCI storage backend docs to reference section 6.3.

2. **CLI Reference** (`cli/docs/reference/`)
   - If CLI flags are added (e.g., `--annotate-ownership`), generate reference docs.
   - Show annotations in `ocm describe componentversion` output.

3. **OCM Spec** ([ocm-spec](https://github.com/open-component-model/ocm-spec))
   - Already defines annotations in section 6.3. No spec changes needed for Option 1.
   - If extra annotations are added (e.g., `software.ocm.component.provider`), submit a spec extension.

4. **Code Documentation**
   - Document annotation constants in [`bindings/go/oci/spec/annotations/annotations.go`](https://github.com/open-component-model/open-component-model/blob/main/bindings/go/oci/spec/annotations/annotations.go) with spec references.
   - Document `Adopt()` parameters in [`bindings/go/oci/internal/identity/descriptor.go`](https://github.com/open-component-model/open-component-model/blob/main/bindings/go/oci/internal/identity/descriptor.go).

5. **ADR Cross-References**
   - Reference from [ADR 0012](./0012_oci_format_compatibility.md) — ownership annotations complement the Index-based format.
   - Reference from [ADR 0008](./0008_signing_verification.md) — annotations interact with signature verification (spec 6.3.3).

### How to Validate

- `ocm describe componentversion` should show ownership annotations on resource artifacts.
- `oras manifest fetch <image-ref>` should show annotations in the manifest JSON.

---

## Open Questions

1. 

---

## Conclusion

