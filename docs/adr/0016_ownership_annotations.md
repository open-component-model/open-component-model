# Ownership Annotations for OCI Image Resources (Asset-to-Owner)

* **Status**: proposed
* **Deciders**: OCM Technical Steering Committee
* **Date**: 2026-03-12

**Technical Story**: Given an OCI artifact stored in a registry, there is no standardized way to trace it back to the OCM component version that owns it. This ADR evaluates approaches to attach ownership metadata (component name, version, artifact identity) to OCI resources to make this possible.

---

## Context and Problem Statement

OCM stores resources (e.g. container images) as standard OCI artifacts in registries. Once stored, these artifacts look like any other image — there is no way to tell which OCM component version they belong to.

This is the **"asset-to-owner" problem**: given an OCI artifact, find the component version that shipped it. Without this link:

- **Security teams** cannot quickly find who owns a vulnerable image.
- **Auditors** must manually cross-reference images to component versions.
- **Platform teams** have no automated way to build an inventory of deployed components.
- **Air-gapped transfers** lose the connection to the originating component version entirely.

---

## Decision Drivers

* Security and compliance tools need to trace artifacts back to their component versions
* The [OCM OCI spec (section 6.3)](https://github.com/open-component-model/ocm-spec/blob/e9273b126045b96e11cc9caf056363728c76bec8/doc/04-extensions/03-storage-backends/oci.md#63-asset-annotations) already defines the annotation keys — we should follow it
* Annotations must not break digest verification or signatures
* Ownership metadata must survive air-gapped transfers
* The solution must work with both the current manifest-based format and the index-based format from [ADR 0012](./0012_oci_format_compatibility.md)

---

## Considered Options

Two approaches are considered for how ownership metadata is stored and discovered:

### 1. Embedded Manifest Annotations

Per the [OCM OCI spec section 6.3](https://github.com/open-component-model/ocm-spec/blob/e9273b126045b96e11cc9caf056363728c76bec8/doc/04-extensions/03-storage-backends/oci.md#63-asset-annotations), implementations **MAY** add ownership annotations to OCI artifacts. If added, they **MUST** be written on the **top-level manifest or index** (`manifest.annotations` or `index.annotations`), not on nested manifests. Only new artifacts packed by OCM are annotated; existing artifacts are not modified.

| Annotation Key | Purpose | Value Format |
| --- | --- | --- |
| `software.ocm.component.name` | Component name | Plain string |
| `software.ocm.component.version` | Component version | Plain string |
| `software.ocm.artifact` | Artifact identity and kind from the CD | JSON (SHOULD be [JCS-canonicalized](https://www.rfc-editor.org/rfc/rfc8785)), e.g. `{"identity":{"name":"my-resource"},"kind":"resource"}` |
| `software.ocm.base.digest` | Digest of the manifest before annotations were injected | `<algorithm>:<hex>` (e.g. `sha256:abc123...`) |

#### Direct Lookup from a Resource Image

Create a component version with an OCI image resource stored by value (`copyPolicy: byValue`), then retrieve the ownership annotations from the registry. The `copyPolicy: byValue` downloads the image from the source registry and re-uploads it as a local blob, injecting ownership annotations into the resource's manifest.

```bash
# 1. Create a component constructor that references an existing OCI image by value
cat > component-constructor.yaml <<EOF
components:
- name: ocm.software/my-component
  version: v1.0.0
  provider:
    name: ocm.software
  resources:
  - name: my-resource
    version: v1.0.0
    type: ociArtifact
    copyPolicy: byValue
    access:
      type: OCIImage/v1
      imageReference: ghcr.io/piotrjanik/open-component-model/hello-ocm:latest
EOF

# 2. Build the component version into the target registry
ocm add component-version \
  --repository ghcr.io/piotrjanik/open-component-model \
  --constructor component-constructor.yaml

# 3. Get the resource's image reference from the component descriptor
IMAGE_REF=$(ocm get component-version ghcr.io/piotrjanik/open-component-model//ocm.software/my-component:v1.0.0 -o json \
  | jq -r '.[0].component.resources[] | select(.type == "ociArtifact") | .access.globalAccess.imageReference')

# 4. Fetch the resource's manifest and print ownership annotations
oras manifest fetch "$IMAGE_REF" | jq '.annotations'
# {
#   "software.ocm.base.digest": "sha256:...",
#   "software.ocm.component.name": "ocm.software/my-component",
#   "software.ocm.component.version": "v1.0.0"
# }
```

**Limitation**: Plain blob resources (non-OCI) have no standalone manifest — they cannot be traced this way. Existing OCI images added by reference (without `copyPolicy: byValue`) are not modified and lack ownership annotations.

#### Validation: Confirm Ownership Against the Component Descriptor

To validate the ownership claim, check that the resource's digest in the CD matches the registry manifest digest:

```bash
# Get the resource digest from the CD
ocm get component-version ghcr.io/piotrjanik/open-component-model//ocm.software/my-component:v1.0.0 -o json \
  | jq -r '.[0].component.resources[] | select(.name == "my-resource") | .access.localReference'
# sha256:b801b8bd...

# Get the manifest digest from the registry
oras manifest fetch --descriptor "$IMAGE_REF" | jq -r '.digest'
# sha256:b801b8bd...
```

If both digests match, the artifact belongs to `ocm.software/my-component:v1.0.0`.

#### Digest Change and `software.ocm.base.digest`

Adding annotations changes the manifest bytes and produces a new digest. The CD and registry both store the **annotated** digest, so they always match. However, this digest differs from the original unannotated artifact.

`software.ocm.base.digest` records the digest **before** annotation injection. Tools can strip the OCM annotations, recompute the digest, and confirm it matches `software.ocm.base.digest` — proving the original artifact is unchanged.

> **OCM signing is unaffected.** Signatures are computed over the normalized Component Descriptor, not individual manifests. The [normalisation algorithm](https://github.com/open-component-model/open-component-model/blob/main/bindings/go/descriptor/normalisation/json/v4alpha1/normalisation.go) includes each resource's `digest` but excludes `access`. Since the CD records the post-annotation digest, signing and verification remain consistent.

#### Implementation Details

The change would write `software.ocm.component.name`, `software.ocm.component.version`, `software.ocm.artifact`, and `software.ocm.base.digest` on the **resource's own manifest/index** (per the spec).

##### Injection Point

Annotations would only be injected when a resource is stored **by value** (e.g. `copyPolicy: byValue` in the constructor, or local file input). Resources stored by reference would not be modified.

The changes would go through the OCI binding library (`bindings/go/oci`):

1. [`repository.go`](https://github.com/open-component-model/open-component-model/blob/main/bindings/go/oci/repository.go) `uploadAndUpdateLocalArtifact` — populate `ManifestAnnotations` in `pack.Options` with `software.ocm.component.name`, `software.ocm.component.version`, and `software.ocm.artifact` (the `component` and `version` parameters are already available).
2. [`pack.go`](https://github.com/open-component-model/open-component-model/blob/main/bindings/go/oci/internal/pack/pack.go) `ResourceLocalBlobOCILayout` — forward `opts.ManifestAnnotations` to `CopyOCILayoutWithIndex`.
3. [`blob_io.go`](https://github.com/open-component-model/open-component-model/blob/main/bindings/go/oci/tar/blob_io.go) `proxyOCIStoreWithTopLevelDescriptor` — capture the pre-annotation digest, unmarshal the top-level manifest/index, merge the annotations, add `software.ocm.base.digest`, re-marshal, and recompute the descriptor.

##### Signature Compatibility

Injecting annotations changes the manifest digest, which **invalidates existing OCI-level signatures** (cosign, Notary). OCM signatures are unaffected (they sign the normalized CD, not individual manifests).

`software.ocm.base.digest` enables verification of the original artifact: strip the OCM annotations, recompute the digest, and confirm it matches `software.ocm.base.digest`. However, full cosign verification also requires the signature artifact (keyed by the original digest) to be available — OCM does not currently transfer cosign signature artifacts.

Resources stored by reference are unaffected — their manifests are not modified.

### 2. OCI Referrers API (Non-Invasive Alternative)

**Problem**: The embedded annotation approach (Option 1) injects ownership annotations directly into the resource's manifest, which changes the digest, invalidates existing OCI-level signatures, and requires the `software.ocm.base.digest` bridging mechanism. This is inherently invasive — the original artifact is modified before it reaches the registry.

OCI Distribution Spec v1.1 introduced the [Referrers API](https://github.com/opencontainers/distribution-spec/blob/v1.1.0/spec.md#listing-referrers) and the [`subject`](https://github.com/opencontainers/image-spec/blob/v1.1.0/manifest.md#image-manifest-property-descriptions) field specifically to associate metadata artifacts with existing images **without modifying the original**.

**Approach**: Instead of injecting annotations into the resource's manifest, OCM pushes a **separate OCI artifact** (the "ownership referrer") whose `subject` field points to the original resource manifest. The ownership annotations live on this referrer artifact, and the original artifact remains byte-for-byte identical.

#### How It Works

1. **Pack the resource as-is** — the OCI layout resource is pushed to the registry without annotation injection. The digest recorded in the CD matches the original, unmodified manifest.
2. **Push an ownership referrer** — a minimal OCI manifest is pushed with:
   - `subject`: a descriptor pointing to the resource's original manifest (digest, mediaType, size).
   - `artifactType`: a dedicated media type, e.g. `application/vnd.ocm.software.ownership.v1+json`.
   - `annotations`: the ownership metadata (`software.ocm.component.name`, `software.ocm.component.version`, `software.ocm.artifact`).

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "artifactType": "application/vnd.ocm.software.ownership.v1+json",
  "config": {
    "mediaType": "application/vnd.oci.empty.v1+json",
    "digest": "sha256:44136fa355b311bfa706...",
    "size": 2
  },
  "layers": [
    {
      "mediaType": "application/vnd.oci.empty.v1+json",
      "digest": "sha256:44136fa355b3678a1146...",
      "size": 2
    }
  ],
  "subject": {
    "mediaType": "application/vnd.oci.image.manifest.v1+json",
    "digest": "sha256:abc123...",
    "size": 1769
  },
  "annotations": {
    "software.ocm.component.name": "github.com/acme/myapp",
    "software.ocm.component.version": "1.2.3",
    "software.ocm.artifact": "{\"identity\":{\"name\":\"my-image\"},\"kind\":\"resource\"}"
  }
}
```

1. **Discovery via Referrers API** — given a resource image reference, a consumer queries:

```http
GET /v2/<name>/referrers/sha256:abc123...?artifactType=application/vnd.ocm.software.ownership.v1+json
```

The registry returns an OCI Index listing the ownership referrer. The consumer reads the annotations to find the owning component version. No OCM-specific tooling is required — any OCI v1.1 client (e.g. `oras discover`) can perform this lookup.

#### Example: Attaching and Discovering Ownership with `oras`

Verified against `ghcr.io/piotrjanik/open-component-model/hello-ocm:latest` (digest `sha256:e22e4bb2...`).

Attach ownership annotations as a referrer to an existing resource image:

```bash
# Attach an empty artifact with ownership annotations to the resource manifest.
# --artifact-type identifies this as OCM ownership metadata.
# The two --annotation flags carry the component name and version.
oras attach ghcr.io/piotrjanik/open-component-model/hello-ocm@sha256:e22e4bb2a42521598d0cddaaca53f5a4354e9d4ebb3a55d604591e3cf30e7836 \
  --artifact-type "application/vnd.ocm.software.ownership.v1+json" \
  --annotation "software.ocm.component.name=acme.org/hello-ocm" \
  --annotation "software.ocm.component.version=1.0.0" \
  --annotation 'software.ocm.artifact={"identity":{"name":"hello-ocm"},"kind":"resource"}'
```

Discover ownership metadata starting from the original resource manifest:

```bash
# List all referrers of the resource image, filtered by artifact type.
oras discover ghcr.io/piotrjanik/open-component-model/hello-ocm@sha256:e22e4bb2a42521598d0cddaaca53f5a4354e9d4ebb3a55d604591e3cf30e7836 \
  --artifact-type "application/vnd.ocm.software.ownership.v1+json" \
  --format json
```

Output:

```json
{
  "referrers": [
    {
      "reference": "ghcr.io/piotrjanik/open-component-model/hello-ocm@sha256:dc9fa2ca583cb70c91389aa010acfe143fd74f8071451914ff868a906bf989a0",
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "digest": "sha256:dc9fa2ca583cb70c91389aa010acfe143fd74f8071451914ff868a906bf989a0",
      "size": 789,
      "annotations": {
        "software.ocm.component.name": "acme.org/hello-ocm",
        "software.ocm.component.version": "1.0.0",
        "software.ocm.artifact": "{\"identity\":{\"name\":\"hello-ocm\"},\"kind\":\"resource\"}"
      },
      "artifactType": "application/vnd.ocm.software.ownership.v1+json"
    }
  ]
}
```

The annotations in the referrer list directly reveal the owning component version — no need to fetch the referrer manifest separately. The referrer manifest itself contains the `subject` field pointing back to the original image:

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "artifactType": "application/vnd.ocm.software.ownership.v1+json",
  "config": {
    "mediaType": "application/vnd.oci.empty.v1+json",
    "digest": "sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a",
    "size": 2
  },
  "layers": [
    {
      "mediaType": "application/vnd.oci.empty.v1+json",
      "digest": "sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a",
      "size": 2
    }
  ],
  "subject": {
    "mediaType": "application/vnd.oci.image.index.v1+json",
    "digest": "sha256:e22e4bb2a42521598d0cddaaca53f5a4354e9d4ebb3a55d604591e3cf30e7836",
    "size": 506
  },
  "annotations": {
    "software.ocm.component.name": "acme.org/hello-ocm",
    "software.ocm.component.version": "1.0.0",
    "software.ocm.artifact": "{\"identity\":{\"name\":\"hello-ocm\"},\"kind\":\"resource\"}"
  }
}
```

#### Validation: Confirm Ownership via Referrers API

To validate ownership, query the Referrers API for the resource's digest and check the annotations:

```bash
# Get the resource image reference from the CD
IMAGE_REF=$(ocm get component-version ghcr.io/piotrjanik/open-component-model//ocm.software/my-component:v1.0.0 -o json \
  | jq -r '.[0].component.resources[] | select(.name == "my-resource") | .access.globalAccess.imageReference')

# Discover ownership referrers
oras discover "$IMAGE_REF" \
  --artifact-type "application/vnd.ocm.software.ownership.v1+json" \
  --format json | jq '.referrers[0].annotations'
# {
#   "software.ocm.component.name": "ocm.software/my-component",
#   "software.ocm.component.version": "v1.0.0"
# }
```

To cross-check, fetch the referrer manifest and verify `subject.digest` matches the CD's `localReference`:

```bash
# subject.digest from the referrer manifest
oras manifest fetch "$REFERRER_REF" | jq -r '.subject.digest'
# sha256:e22e4bb2...

# localReference from the CD
ocm get component-version ghcr.io/piotrjanik/open-component-model//ocm.software/my-component:v1.0.0 -o json \
  | jq -r '.[0].component.resources[] | select(.name == "my-resource") | .access.localReference'
# sha256:e22e4bb2...
```

If both digests match, the referrer is authentic. Unlike Option 1, the resource digest is the **original unmodified** digest — no `software.ocm.base.digest` bridging is needed.

#### Implementation Details

##### Injection Point — Creation

After a resource is uploaded to the registry, push a second OCI manifest (the ownership referrer) into the **same repository**.

1. **[`annotations.go`](https://github.com/open-component-model/open-component-model/blob/main/bindings/go/oci/spec/annotations/annotations.go)** — add constants for the ownership annotation keys and the artifact type `application/vnd.ocm.software.ownership.v1+json`. The artifact type follows the [OCI artifact type convention](https://github.com/opencontainers/image-spec/blob/v1.1.0/manifest.md#guidelines-for-artifact-usage) and enables filtering via the Referrers API (`?artifactType=...`), distinguishing ownership referrers from other attached artifacts (cosign signatures, SBOMs, etc.).
2. **[`repository.go`](https://github.com/open-component-model/open-component-model/blob/main/bindings/go/oci/repository.go) `uploadAndUpdateLocalArtifact`** — after `pack.ArtifactBlob` returns the resource descriptor (`desc`), build and push the referrer manifest. The `component` and `version` parameters are already available in this function.
3. **Build the `application/vnd.ocm.software.ownership.v1+json` referrer manifest** — a minimal `ocispec.Manifest` with:
   - `Subject`: descriptor of the resource (`desc.MediaType`, `desc.Digest`, `desc.Size`)
   - `ArtifactType`: `application/vnd.ocm.software.ownership.v1+json`
   - `Config`: `ocispec.DescriptorEmptyJSON` (standard empty `{}`)
   - `Layers`: single `ocispec.DescriptorEmptyJSON` entry (required by the [OCI artifact guidance](https://github.com/opencontainers/image-spec/blob/v1.1.0/manifest.md#guidelines-for-artifact-usage))
   - `Annotations`: `software.ocm.component.name`, `software.ocm.component.version`, and `software.ocm.artifact`
4. **Push via ORAS** — marshal the manifest to JSON, compute the descriptor (`digest.FromBytes`), then call `store.Push(ctx, manifestDesc, bytes.NewReader(manifestJSON))`. The `store` is a `content.Storage` (ORAS interface) — the same store used for the resource itself. When pushing to a remote registry, ORAS automatically handles:
   - **Referrers API (v1.1)**: the registry detects the `subject` field and indexes the referrer. The response includes the `OCI-Subject` header confirming support.
   - **Tag fallback (v1.0)**: ORAS automatically falls back to the [referrers tag schema](https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md#unavailable-referrers-api) for registries that don't support the Referrers API.

##### Injection Point — Transfer

`ocm transfer` currently does **not** copy ownership referrers. The transfer graph is built in [`graph.go`](https://github.com/open-component-model/open-component-model/blob/main/cli/cmd/transfer/component-version/internal/graph.go) `fillGraphDefinitionWithPrefetchedComponents`, which processes resources via `processLocalBlob` / `processOCIArtifact` but has no referrer discovery step.

Two strategies to handle referrers during transfer:

1. **Re-create on upload** — if the target repository's `uploadAndUpdateLocalArtifact` pushes a fresh referrer (as described above), then any resource that goes through the full upload path will automatically get a referrer in the target. This is the simplest approach.
2. **Discover and copy** — for resources copied without re-uploading (e.g. blob-level copy or OCI layout transfer), the referrer must be explicitly transferred. Use `registry.Referrers()` to discover ownership referrers, then copy them:

```go
// Discover ownership referrers for a resource descriptor
const ownershipArtifactType = "application/vnd.ocm.software.ownership.v1+json"

referrers, err := registry.Referrers(ctx, sourceStore, resourceDesc, ownershipArtifactType)
if err != nil {
    return fmt.Errorf("failed to discover ownership referrers: %w", err)
}

// Copy each referrer manifest to the target
for _, ref := range referrers {
    rc, err := sourceStore.Fetch(ctx, ref)
    if err != nil {
        return fmt.Errorf("failed to fetch referrer %s: %w", ref.Digest, err)
    }
    defer rc.Close()

    if err := targetStore.Push(ctx, ref, rc); err != nil {
        return fmt.Errorf("failed to push referrer %s: %w", ref.Digest, err)
    }
}
```

#### Consuming Ownership Referrers from External Tools

Once OCM tooling pushes ownership referrers (`application/vnd.ocm.software.ownership.v1+json`) alongside resource images, any OCI v1.1-compatible tool can discover and consume them **without OCM-specific libraries**. This section describes how external tools — image replicators, vulnerability scanners, admission controllers, GitOps operators, and platform dashboards — can integrate with ownership referrers.

##### Discovery

Given a resource image digest, query the registry's Referrers API filtered by artifact type:

```http
GET /v2/<name>/referrers/<digest>?artifactType=application/vnd.ocm.software.ownership.v1+json
```

The response is an OCI Index whose `manifests` array contains descriptors for each matching referrer. The ownership annotations (`software.ocm.component.name`, `software.ocm.component.version`, `software.ocm.artifact`) are inlined in each descriptor's `annotations` field — no need to fetch the referrer manifest separately.

With standard tooling:

```bash
oras discover <registry>/<repo>@<digest> \
  --artifact-type "application/vnd.ocm.software.ownership.v1+json" \
  --format json | jq '.referrers[0].annotations'
# {
#   "software.ocm.component.name": "github.com/acme/myapp",
#   "software.ocm.component.version": "1.2.3",
#   "software.ocm.artifact": "{\"identity\":{\"name\":\"my-image\"},\"kind\":\"resource\"}"
# }
```

##### Transferring Referrers During Image Replication

Tools that replicate OCI images between registries (e.g. image mirrors, air-gapped transfer pipelines, component transport tools) must be extended to copy ownership referrers alongside the resource image:

1. **Discover** — after uploading the resource image to the target registry, query the **source** registry's Referrers API for the resource's digest, filtered by `artifactType=application/vnd.ocm.software.ownership.v1+json`.

2. **Fetch** — for each referrer descriptor returned, fetch its full manifest from the source registry. An ownership referrer manifest is a standard OCI manifest with an empty config (`application/vnd.oci.empty.v1+json`, `{}`) and an empty layers array — there are no blobs to transfer beyond the manifest itself.

3. **Re-push** — push each referrer manifest to the **target** registry under the same repository as the resource image. The `subject.digest` in the referrer already matches the resource's digest (which is unchanged, since Option 2 does not modify the original manifest). When the target registry receives a manifest with a `subject` field, it automatically indexes it in its referrers list — no additional API calls are needed.

This requires the replication tool's OCI client to support listing referrers for a given digest and pushing manifest-only artifacts.

##### Validating Ownership After Transfer

After transfer, consumers can verify that the ownership referrer correctly points to the resource image:

1. **Digest match** — fetch the referrer manifest from the target registry and check that `subject.digest` equals the resource image's manifest digest. This confirms the referrer is bound to the correct image.

2. **Annotation check** — read `software.ocm.component.name` and `software.ocm.component.version` from the referrer's `annotations` and confirm they match the expected component version from the component descriptor.

3. **Referrers API round-trip** — query the target registry's Referrers API for the resource digest and confirm the ownership referrer appears in the response:

   ```bash
   oras discover <target-registry>/<repo>@<resource-digest> \
     --artifact-type "application/vnd.ocm.software.ownership.v1+json" \
     --format json | jq '.referrers[0].annotations'
   ```

   If the referrer is listed with the correct annotations, the transfer is complete and the ownership link is intact.

#### Fallback Compatibility with Pre-v1.1 Registries

The Referrers API was introduced in OCI Distribution Spec v1.1. Registries that have not yet adopted v1.1 do not natively index manifests by their `subject` field, so the `GET /v2/<name>/referrers/<digest>` endpoint is unavailable. The OCI spec defines a [referrers tag schema](https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md#unavailable-referrers-api) as a fallback for this case.

##### How the Tag Fallback Works

When a client pushes a manifest with a `subject` field and the registry responds **without** the `OCI-Subject` header (indicating it does not support the Referrers API), the client must maintain a **referrers tag** — a special OCI Index stored as a tagged manifest in the same repository:

1. **Tag format** — the tag is derived from the subject digest: `<algorithm>-<hex>`. For example, a subject with digest `sha256:abc123...` produces the tag `sha256-abc123...`.
2. **Push flow** — after pushing the ownership referrer manifest, the client fetches the existing referrers index at that tag (or creates an empty one), appends the new referrer descriptor to its `manifests` array, and pushes the updated index back under the same tag.
3. **Discovery flow** — consumers query `GET /v2/<name>/manifests/sha256-<hex>` to fetch the referrers index, then read the `manifests` array exactly as they would from the Referrers API response.

##### ORAS Handles This Automatically

The ORAS Go library ([`oras.land/oras-go`](https://github.com/oras-project/oras-go)) detects whether the target registry supports the Referrers API during push. If the registry does not return the `OCI-Subject` header, ORAS automatically falls back to the tag schema — no additional code is needed in the OCM implementation. The same applies to `oras discover` on the client side: it transparently checks both the Referrers API and the tag fallback. The fallback logic is implemented in [`registry/remote/repository.go`](https://github.com/oras-project/oras-go/blob/v2.6.0/registry/remote/repository.go) (`pushWithIndexing`, `updateReferrersIndex`) with supporting types in [`registry/remote/referrers.go`](https://github.com/oras-project/oras-go/blob/v2.6.0/registry/remote/referrers.go) (`buildReferrersTag`).

##### Registry Support Matrix

| Registry | Referrers API (v1.1) | Tag Fallback |
| --- | --- | --- |
| GitHub Container Registry (ghcr.io) | ✅ | ✅ |
| Docker Hub | ❌ | ✅ |
| Azure Container Registry | ✅ | ✅ |
| Amazon ECR | ❌ | ✅ |
| Google Artifact Registry | ✅ | ✅ |
| Harbor (v2.6+) | ✅ | ✅ |
| Zot | ✅ | ✅ |

---

### Comparison of Options 1 and 2

| Aspect | Embedded Annotations (Option 1) | Referrers API (Option 2) |
| --- | --- | --- |
| Original digest preserved | ❌ No — `software.ocm.base.digest` bridge needed | ✅ Yes — artifact untouched |
| Self-contained | ✅ Annotations travel with the manifest | ❌ Referrer is separate manifest |
| OCI-level signatures | ❌ Invalidated (cosign, Notary) | ✅ Preserved |
| Transfer | ✅ Automatic — no extra copy step | ❌ Referrer must be copied separately |
| Air-gapped / OCI layout | ✅ Embedded in manifest | ❌ Referrer must be included in layout tarball |
| Artifact count | ✅ 1 per resource | ❌ 2 per resource (resource + referrer) |
| Discovery without OCM | ✅ Read `manifest.annotations` | ✅ `oras discover` / `GET /referrers/<digest>` |
| Registry compatibility | ✅ Works with any OCI registry | ❌ Requires OCI Distribution v1.1 (or tag fallback) |

### Support in Legacy OCM CLI

Ownership annotations are a new feature. New features are developed in the new OCM tooling (`open-component-model/open-component-model`), which is the most recent and actively developed version of OCM. The legacy CLI (`open-component-model/ocm`) is in maintenance mode and will not receive this feature. It will gain **read support** for the new index-based format ([ADR 0012](./0012_oci_format_compatibility.md)), which naturally preserves any annotations already present on resource manifests.

**Decision**: Implement only in the new OCM tooling.

## Decision

**Option 2 (OCI Referrers API)** is chosen as the primary approach. Ownership metadata is stored as a separate OCI referrer artifact linked to the resource via the `subject` field, leaving the original resource manifest untouched. This preserves the original digest, keeps OCI-level signatures (cosign, Notary) valid, and removes the need for the `software.ocm.base.digest` bridging mechanism.

### Steps

1. **Implement referrer creation** — after a resource is uploaded to the registry, push an ownership referrer artifact (`application/vnd.ocm.software.ownership.v1+json`) with `subject` pointing to the resource's original manifest. The referrer carries `software.ocm.component.name`, `software.ocm.component.version`, and `software.ocm.artifact` in its annotations. ORAS handles Referrers API vs tag fallback automatically.

2. **Implement referrer transfer** — ensure `ocm transfer` copies ownership referrers alongside resources. Either re-create referrers on upload (simplest) or discover and copy them from the source registry.

3. **Document**:

   - **Code** — annotation constants and artifact type in [`annotations.go`](https://github.com/open-component-model/open-component-model/blob/main/bindings/go/oci/spec/annotations/annotations.go) with spec references.
   - **OCM Website** ([ocm.software](https://ocm.software)) — add a concepts/how-to page on ownership annotations and update [OCI storage backend](https://ocm.software/docs/) docs. Source: [`ocm-website`](https://github.com/open-component-model/ocm-website) under `content/docs/`.

4. **E2E tests**:

   - **Creation** — Create a CV with an OCI layout resource, verify `oras discover` returns the ownership referrer with correct annotations.
   - **Transfer** — Transfer CV between registries, verify the ownership referrer exists on the target.
   - **Tracing** — Given a resource image ref, discover the ownership referrer via Referrers API and extract component name/version.
