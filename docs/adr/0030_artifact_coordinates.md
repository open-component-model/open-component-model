# ADR: Portable Artifact Coordinates and Typed Upload Configuration

- **Status**: proposed (decision pending)
- **Deciders**: SIG Runtime, Fabian Burth (@fabianburth)
- **Date**: 2026-09-18

## Context and Problem

Converting external access to `localBlob` loses upload coordinates. The receiver knows its destination endpoint but cannot reliably reconstruct artifact naming from the OCM resource name/version. Naming must survive local storage, air-gapped transfer, and intermediate external storage.

[EPIC #1264](https://github.com/open-component-model/ocm-project/issues/1264) starts with same-technology uploads and proposes a hub-and-spoke model for cross-technology uploads. Adding a source should not require new mappings in every target.

This ADR refines [ADR-0018](0018_transfer_configuration.md) and compares three naming contracts. All support typed destination configuration.

## Decision Status

No option has been selected. API sketches are illustrative, not implemented or finalized.

1. [Specialized coordinate families](#option-1-specialized-coordinate-families)
2. [Unified coordinates](#option-2-unified-coordinates)
3. [Hybrid coordinates through a shared hub](#option-3-hybrid-coordinates-through-a-shared-hub)

The [appendix](#appendix-detailed-native-api-draft) retains the native API draft for reference. It is not an approved contract.

## Decision Drivers

- Preserve enough naming information for low-configuration same-technology uploads
- Prepare for cross-technology publishing without coupling every target to every source
- Preserve native naming semantics where they matter, without promising impossible lossless mappings
- Support disconnected and multi-hop transfers, including deployments with different installed adapters
- Keep naming separate from payload representation, integrity, provenance, and destination infrastructure
- Make missing information, incompatible representations, ambiguous policy, and collisions explicit errors
- Prefer a public contract that is small enough to understand and precise enough to validate

## Common Requirements and Boundaries

### Separate Naming from Content and Destination

```text
portable naming + compatible payload + destination configuration
                              ↓
                         target access
```

| Concern        | Responsibility                                                                                                                            |
| -------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| Naming         | Retain artifact names, publication aliases, and distribution names where known. Do not invent them from OCM resource metadata.            |
| Representation | Identify and validate actual content: chart archive, OCI layout, arbitrary bytes, etc. A downloader type alone does not establish format. |
| Integrity      | Verify resource digests independently. An archive digest, OCI root digest, Git commit, and S3 version ID have different meanings.         |
| Destination    | Typed config provides endpoint, registry prefix, bucket, repository, and upload policy. Credentials use the existing resolver.            |
| Provenance     | Keep separate from naming and destination authority.                                                                                      |

A chart downloaded through S3 or HTTP can be eligible for Helm publication after inspection. An arbitrary blob cannot become a chart because its coordinates contain a name and version. Helm-to-OCI conversion and OCI-layout extraction remain explicit format operations under every option.

An OCI tag is not necessarily a release version. S3 version IDs are bucket-assigned and cannot be replayed on upload. Digest-only OCI publication must remain possible without inventing `latest`. A GitHub archive cannot recreate Git history. Wget defines a download mechanism, not a generic upload protocol: HTTP PUT, WebDAV, and Artifactory need concrete backends.

### Destination Configuration and Execution

All options can use the same destination-facing configuration shape:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    copyMode: allResources
  - type: oci.upload.config.ocm.software/v1alpha1
    baseUrl: registry.internal/mirror
```

Keep configuration separate from coordinates and resource destinations independent of the component repository. Select an uploader by explicit policy and capabilities, not coordinate list order. Ambiguous selection fails. A selected upload failure is not a fallback signal.

Reuse the transformation planner and execution graph. Preserve OCI streaming and validate output-dependent facts before upload. Reject unsupported backend configuration. CLI and controller should share planning behavior and account for destination-policy changes when deciding whether to skip replication.

### Persistence, Integrity, and Safety

All options need a versioned persistent contract. Examples use an unsigned `ocm.software/artifact-coordinates` label. Choose a list or object schema, or require explicit migration between them.

- Preserve unknown metadata during local copies, but require understood semantics for consumption
- Apply changes to copied output resources and leave source metadata unchanged on failure
- Remove metadata only after successful publication and only if the resulting access can reconstruct all information being removed
- Intermediate storage must not erase known semantic identity. An S3 access alone generally does not preserve a chart's name/version
- Revalidate naming metadata after content conversion. Do not claim automatic signature preservation or transactional rollback of already uploaded content
- Never automatically mutate signing-relevant labels. Preserve unrelated metadata
- Treat coordinates as untrusted data, not CEL source or permission to choose an endpoint. Validate target names and detect collisions
- Do not silently normalize opaque S3 keys, decode HTTP escapes, lowercase names, or resolve traversal. Target-specific rendering may reject unrepresentable names or require explicit policy

Decide whether to retain logical names across uploads or rebase them to the new access. Never guess which part of an external path was a previous destination prefix.

## Option 1: Specialized Coordinate Families

### Model and Example

Persist a list of typed native coordinates. Each uploader consumes its own family. Multiple entries can describe publication alternatives for one payload.

```yaml
labels:
  - name: ocm.software/artifact-coordinates
    signing: false
    value:
      - type: HelmChartCoordinates/v1alpha1
        name: payments
        version: 1.4.0+build.1
      - type: OCIArtifactCoordinates/v1alpha1
        repository: team/charts/payments
        tag: 1.4.0_build.1
```

```go
// Sketch: policy selects the uploader; it consumes its native family.
helmCoordinates, err := coordinates.Find[HelmChartCoordinates](resource)
if err != nil {
    return err
}
return helmUploader.Upload(ctx, helmCoordinates, chartArchive)
```

The list does not imply format compatibility. In this example, publishing to OCI still requires a chart-to-OCI conversion. Reject duplicate family names across versions and never use list order as precedence.

### Extensibility and Persistence

Same-technology transfer has a direct contract: download produces native coordinates and upload consumes them. A Helm-to-OCI converter can produce an additional OCI family.

Cross-technology upload needs a producer for the target's family. Without a shared model, adapters may accumulate pairwise mappings. Adding a mandatory shared model later would move this design toward Option 3.

Persist compatible native entries across local hops and intermediate external storage. Regenerate current-access metadata without discarding valid alternatives. Consumption and cleanup operate per family, subject to the common integrity and persistence requirements.

### Pros

- Precise schemas express native naming semantics and validation directly
- Families can evolve independently
- Straightforward initial same-technology implementation
- Native details need not be squeezed into a common naming model

### Cons

- Generic targets cannot consume arbitrary source families directly
- Cross-technology naming needs additional mappings or a later architectural extension
- Multiple families add selection, compatibility, and cleanup rules
- The epic's no-pairwise-mappings goal remains unresolved if this is the final architecture

## Option 2: Unified Coordinates

### Model and Example

Sources normalize to one shared contract. Uploaders validate and render it for their target without inspecting the source type.

```text
source access → normalizer → shared coordinates → target renderer → target access
                                    ↑
                          verified payload metadata
```

A candidate model, not a complete schema:

```go
type ArtifactCoordinates struct {
    Type      runtime.Type `json:"type"`
    Namespace []string     `json:"namespace,omitempty"`
    Name      string       `json:"name,omitempty"`
    Version   string       `json:"version,omitempty"`
    Tag       string       `json:"tag,omitempty"`
    Path      *string      `json:"path,omitempty"`
}
```

| Field       | Shared meaning                                                                                                                                   |
| ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| `namespace` | Logical naming segments without an endpoint or URL escaping.                                                                                     |
| `name`      | Artifact name, from authoritative payload metadata when available.                                                                               |
| `version`   | Artifact release version, excluding arbitrary tags, Git commits, and S3 version IDs.                                                             |
| `tag`       | Publication alias independent of release version. Never implicitly `latest`.                                                                     |
| `path`      | Optional opaque distribution name for byte storage, not a URL or filesystem path. Presence distinguishes an empty path from missing information. |

```yaml
labels:
  - name: ocm.software/artifact-coordinates
    signing: false
    value:
      type: ArtifactCoordinates/v1alpha1
      namespace: [team, charts]
      name: payments
      version: 1.4.0+build.1
      path: team/charts/payments-1.4.0+build.1.tgz
```

A chart inspector supplies verified name/version regardless of the downloader. Namespace and distribution naming come from normalization or explicit policy. Changing from a chart archive to an OCI-layout serialization must revalidate the distribution filename. OCI root-digest verification remains representation-specific, outside this naming sketch.

```go
// Sketch: no source-type dispatch in a generic byte-storage uploader.
func (u *S3Uploader) Upload(ctx context.Context, c ArtifactCoordinates, p Payload) (Access, error) {
    key, err := u.naming.ObjectKey(c, p)
    if err != nil {
        return nil, err
    }
    // Validate the destination key and collisions before upload.
    // Literal prefixing: do not filesystem-normalize opaque S3 keys.
    return u.put(ctx, u.config.KeyPrefix+key, p)
}
```

`ObjectKey` uses an existing distribution name or a documented policy based on shared fields. It cannot silently invent missing identity. A Helm uploader requires a validated chart archive and checks that shared name/version agree with `Chart.yaml`. Helm's version-to-OCI-tag mapping belongs to format adaptation, not a generic rule for all versions.

### Extensibility and Persistence

A new downloader implements one normalizer. Compatible targets remain unchanged. New naming concepts may require schema changes, and new formats may need inspectors or converters.

Persist the shared object so receivers can interpret naming without the source adapter. Retain it when target access cannot reconstruct its meaning. Retained semantic fields must agree with verified payload facts. Storage paths must not overwrite artifact identity.

S3 normalization can preserve the exact key. HTTP normalization must define how escaped paths, empty paths, and the leading slash map to distribution names. Query-selected resources can collide. HTTP publishers must encode names safely. Preserving the original URL spelling may need separate metadata.

### Pros

- Establishes the hub-and-spoke boundary directly, avoiding pairwise naming mappings
- Generic byte-storage targets consume naming from any compatible source
- One persistent naming contract simplifies source-independent offline consumption
- Keeps uploader eligibility based on naming information and content capabilities

### Cons

- Requires precise shared semantics before stabilizing the public API
- A minimal schema loses native detail, while many optional fields risk ambiguity
- Native round trips may need separate metadata or explicit policy
- New identity concepts can require shared schema evolution, rather than independent family evolution
- Coverage of package variants, Maven classifiers, aliases, and multi-file publications is unproven

## Option 3: Hybrid Coordinates Through a Shared Hub

### Model and Example

Keep specialized coordinates at the boundaries and map cross-technology naming through a shared model. As in Option 2, the model defines common semantics rather than storing opaque native payloads.

```text
specialized source coordinates
              ↓ normalize
       central coordinates
              ↓ project with target policy and validated payload facts
specialized target coordinates
              ↓ upload
         target access
```

```go
// Sketch: each technology implements only its mappings to/from the hub.
// The helper types represent naming policy and verified payload facts.
type CoordinateAdapter[T any] interface {
    Normalize(native T, facts PayloadFacts) (CentralCoordinates, error)
    Project(central CentralCoordinates, facts PayloadFacts, policy NamingPolicy) (T, error)
}

central, err := httpAdapter.Normalize(httpCoordinates, payloadFacts)
if err != nil {
    return err
}
s3Coordinates, err := s3Adapter.Project(central, payloadFacts, namingPolicy)
if err != nil {
    return err
}
return s3Uploader.Upload(ctx, s3Coordinates, payload)
```

The S3 projector consumes central coordinates without knowing HTTP coordinates. Technologies implement only the directions they support. Mappings are not guaranteed inverses. Missing or incompatible information requires policy or an error.

Same-technology transfers can use native coordinates directly to preserve details outside the hub. The same policy, collision, and integrity rules apply, including naming overrides. Format conversion remains separate.

### Extensibility and Persistence

New sources using existing shared semantics leave compatible targets unchanged. Native schemas and uploader contracts can remain in place.

Two persistence variants require an explicit choice:

| Variant                                                 | Benefit                                                                                                  | Cost                                                                                                                                                   |
| ----------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Persist native metadata and derive the hub during planning. | Reuses a native-family label and avoids duplicated shared fields.                                        | Offline consumption requires the relevant source-family normalizer. Multiple families need deterministic reconciliation. Array order is not authority. |
| Hub persisted with optional native details.                 | Compatible targets need not have the original source adapter. Native details retain round-trip fidelity. | Requires an envelope/versioning design and consistency rules for duplicated information.                                                               |

A persisted hub governs cross-technology projection. Reject conflicting overlapping native values and verify payload-defined identity against the content. Overrides and conversions update or invalidate affected fields on the output copy.

An ephemeral hub requires consistent normalization across native families or explicit policy to resolve alternatives. Both variants must preserve identity through intermediate storage and define whether names are retained or rebased.

### Pros

- Combines native validation and round-trip details with a common cross-technology boundary
- Targets remain technology-focused without learning every source's naming scheme
- Can reuse specialized uploader contracts and introduce adapters incrementally
- Does not force every native detail into the central schema

### Cons

- Introduces more concepts: native types, shared semantics, adapters, and potentially duplicate persisted data
- Still requires a shared semantic model
- Projection may be lossy or impossible without retained information or explicit policy
- Conflict resolution, precedence, and conversion invalidation are more involved
- Persistence trades offline adapter dependencies for schema and consistency complexity

## Comparison

| Criterion                          | Option 1: specialized                                                 | Option 2: unified                                  | Option 3: hybrid                                                            |
| ---------------------------------- | ------------------------------------------------ | -------------------------------------------------- | --------------------------------------------------------------------------- |
| Native validation and fidelity     | Directly represented.                            | Target validation, possibly with extra metadata.   | Directly represented at native boundaries.                                  |
| Cross-technology naming            | Additional mappings or later normalization.                           | Mandatory shared contract.                         | Mandatory shared boundary between native types.                             |
| New source with existing semantics | Other target families need coordinate producers. | One normalizer, no compatible target changes.      | One adapter, no compatible target changes.                                  |
| Offline requirements               | Relevant native consumers/converters.            | Shared-schema support plus target support.         | Source normalizer or persisted shared schema.                               |
| Schema evolution                   | Per family.                                                           | Shared schema.                                     | Shared schema plus native families.                                         |
| Primary complexity                 | Multiple families and cross-family mappings.                          | Defining sufficiently expressive common semantics. | Common semantics plus native/shared consistency.                            |
| Initial same-technology scope      | Most direct.                                                          | Requires shared-model design first.                | Native path can ship first, but the common boundary needs early validation. |
| Universal format conversion        | Not provided.                                                         | Not provided.                                      | Not provided.                                                               |

Options 2 and 3 avoid up to N × M naming mappings. Neither provides universal format conversion. Option 1 defers the epic's normalization goal.

## Evaluation Before Selection

Evaluate each option against these cases, including where policy or extra metadata is needed.

| Case                                        | Question to answer                                                                                                                              |
| ------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| OCI → local → OCI                           | Are repository, tag-only, digest-only, and tag-plus-digest semantics preserved?                                                                 |
| HTTP → S3                                   | Can a generic target consume the source's naming without HTTP-specific logic? How are escaped paths, empty paths, and query collisions handled? |
| Chart downloaded through S3 → Helm          | Can content inspection supply verified chart identity independently of storage origin?                                                          |
| Helm → local → S3 → local → Helm            | What is persisted, which adapters are needed offline, and when may metadata be removed?                                                         |
| OCI → S3 → OCI                              | Is a defined OCI-layout serialization preserved along with enough naming information to restore native publication?                             |
| Repeated uploads with destination prefixes  | Are names deliberately retained or rebased, without guessing or unintended prefix accumulation?                                                 |
| New test-only downloader                    | Does producing supported shared semantics require edits to compatible target code? Option 1 must identify the missing bridge.                   |
| Conflicting fields and invalid target names | Is rejection deterministic, with no source-type fallback or silent renaming?                                                                    |
| Content conversion                          | Which coordinates become invalid, and how are representation digests and signatures handled?                                                    |

Golden schema tests, unknown-version copies, signing guards, failure isolation, collision checks, and streaming/buffered parity remain necessary. Prototype normalization and rendering independently of live backends before finalizing the public schema.

## Open Questions for Discussion

1. Is source-independent cross-technology naming a constraint on the first public API, or an explicitly deferred redesign?
2. Do uploaders benefit enough from public native coordinate types to justify Option 3 over Option 2?
3. Must an offline destination interpret portable naming without the original source adapter? If so, Option 3 needs a persisted hub rather than only native metadata
4. What fidelity is required: semantic identity and usable target names, or exact original native naming as well?
5. Which shared fields are sufficient for the supported cases, and how are aliases, package variants, and multi-file artifacts represented?
6. Should logical naming survive every content-preserving hop, or should publication intentionally rebase it to the new access?
7. What naming decisions may defaults make, and when must we require an explicit override?

After discussion, record the choice, persistence rules, naming semantics, and reasons for rejecting the alternatives.

## Delivery Scope and Orthogonal Choices

Initial production support can remain OCI → local → configured OCI upload, Helm-to-OCI support, and streaming parity. S3 and JFrog Helm publication are separate backend increments. Prototype cross-technology naming now without promising general cross-technology publishing in the first increment.

Storage location is a separate choice from coordinate architecture: a label avoids extending `LocalBlob`, while a dedicated access/spec field needs a specification change. Graph-only metadata is insufficient for disconnected transfers. Matchers and target templates are routing features, not substitutes for preserving naming.

The first increment excludes per-resource routing, user CEL, multiple destinations per uploader, general HTTP publishing, Maven APIs, arbitrary reverse conversions, and blob-to-OCI wrapping. Existing blobs may lack naming metadata. Constructors need explicit metadata propagation alongside blob data.

## Appendix: Detailed Native API Draft

This draft describes Option 1. Option 3 could reuse its native types with revised lifecycle rules. Option 2 needs a different coordinate schema.

All details below are conditional, including API names, cleanup, and prefix behavior. Backend constraints apply across options. Reconcile these details with the selected architecture before implementation.

### Access-Type Coverage

Download support does not imply a corresponding uploader.

| Access | Downloaded representation | Coordinate-list entry | Upload configuration |
| ------------------ | --------------------------------------------------------------------- | --------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| `OCIImage/v1` | OCI layout containing the selected artifact graph | `OCIArtifactCoordinates/v1alpha1` | `oci.upload.config.ocm.software/v1alpha1` |
| `Helm/v1`          | Native chart `.tgz` with optional provenance, or converted OCI layout | `HelmChartCoordinates/v1alpha1`. Add `OCIArtifactCoordinates/v1alpha1` after conversion | `jfrog.helm.upload.config.ocm.software/v1alpha1` for classic JFrog. OCI config after conversion |
| `S3/v2`            | Exact object bytes                                                    | `S3ObjectCoordinates/v1alpha1`                                                          | `s3.upload.config.ocm.software/v1alpha1`. Requires a new upload backend                         |
| `Wget/v1` | HTTP response body | `HTTPResourceCoordinates/v1alpha1` | None: download access does not specify an upload protocol |
| `GitHub/v1` | Exact commit tarball, `application/x-tgz` | `GitHubSourceCoordinates/v1alpha1` | None: a tarball cannot recreate a Git repository/commit |
| `LocalBlob/v1`     | Existing stored representation                                        | Preserve existing coordinates                                                           | Selected by coordinates, otherwise existing local storage behavior                              |
| `OCIImageLayer/v1` | Raw layer, not an OCI artifact                                        | No new producer in this increment                                                       | None: no standalone transfer handler. Durable upload needs a referencing manifest               |
| `File/v1alpha1` | Transformation staging file | Preserve resource coordinates alongside file | None: internal blob locator |

Existing schemes resolve access aliases. New coordinate/config types accept only canonical, versioned names. Classic Helm has no standard upload protocol, so JFrog needs a vendor-specific uploader. Maven is outside the current Go implementation and this API contract.

### Shared Artifact Coordinates Label

The label value is a list. This allows one resource to retain coordinates for several target access families without making list order a policy mechanism.

Suggested generic representation in `bindings/go/descriptor/coordinates`:

```go
type List []*runtime.Raw
```

Each entry remains independently typed and is decoded by its technology scheme.

```yaml
labels:
  - name: ocm.software/artifact-coordinates
    signing: false
    value:
      - type: OCIArtifactCoordinates/v1alpha1
        repository: acme/payments
        tag: "1.4.0_build.1"
      - type: HelmChartCoordinates/v1alpha1
        name: payments
        version: "1.4.0+build.1"
```

The envelope could later support Maven coordinates.

- Exactly one reserved label. Its value is a JSON/YAML array of typed coordinate payloads. Each payload `type` owns versioning. Omit `Label.Version`
- At most one entry per target access family. The family key is `runtime.Type.Name`. Versions of the same name therefore compete. Reject duplicate type names, regardless of version. Producers upsert their family entry rather than append blindly
- Upload policy selects the uploader, which selects its coordinate family. List order has no precedence
- If multiple configured uploaders match a resource, fail unless configuration explicitly resolves the ambiguity. Never choose the first coordinate entry
- Coordinate types define naming and representation semantics. They are not partial access specs and have no nested `access`, generic `properties`, or extra `format` field
- Multiple coordinate entries do not imply conversion support. An entry is eligible only when the current blob representation is compatible with the selected uploader or a supported conversion produces that representation
- Generated labels are unsigned. Reject automatic replacement/removal of signing-relevant labels
- Known payloads: reject unknown fields and invalid values when producing or consuming. Opaque local copies preserve unknown types/versions without interpreting them
- Resource-aware transformers attach coordinates on copied output resources. `DownloadResource` remains a blob-returning API and must not mutate input metadata
- Blob media type stays in `File.mediaType` / `LocalBlob.mediaType`. Integrity stays in `resource.digest`. Do not duplicate these in every coordinate entry
- Construct allowlisted relative fields rather than copying/redacting entire accesses. Coordinates are untrusted data, not CEL source or authority to choose an endpoint

### OCI API

Suggested package: `bindings/go/oci/spec/coordinates/v1alpha1`.

```go
type Coordinates struct {
    Type       runtime.Type `json:"type"`
    Repository string       `json:"repository"`
    Tag        string       `json:"tag,omitempty"`
    Digest     string       `json:"digest,omitempty"`
}
```

Wire type: **`OCIArtifactCoordinates/v1alpha1`**.

| Field | Contract |
| ------------ | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| `repository` | Required, registry-relative OCI repository path. No leading/trailing slash, host, scheme, tag, or digest. Validate using OCI repository grammar. |
| `tag` | Optional valid OCI tag. Never derive from OCM resource version. |
| `digest` | Optional full OCI root digest, including algorithm. Not the digest of the layout tar archive. |

At least one of `tag`/`digest` is required. Preserve both when present. Digest pins content. Tag requests publication under a name.

```text
source:      registry.example:5000/team/app:1.4.0@sha256:<digest>
coordinates: repository=team/app, tag=1.4.0, digest=sha256:<digest>
```

Parse a **full source reference** with the existing OCI parser. Copy its repository/tag/digest. Do not parse a relative multi-segment repository as a full reference: that loses its first segment. A tagless/digestless source needs its selected root digest before valid coordinates can be produced. Never invent `latest`.

#### OCI Upload Config

Suggested package: `bindings/go/oci/spec/config/upload/v1alpha1`.

```go
type Config struct {
    Type    runtime.Type `json:"type"`
    BaseURL string       `json:"baseUrl"`
}
```

```yaml
type: oci.upload.config.ocm.software/v1alpha1
baseUrl: registry.internal/mirror
```

- Required authority plus optional repository prefix. Accept explicit `http://` or `https://`. No scheme means HTTPS. Reject credentials, query, fragment, tag, digest, and missing host
- A port belongs to the authority, not an artifact tag. Normalize a trailing slash. Reject empty/internal dot segments in the repository prefix
- Build `<baseUrl>/<coordinates.repository>[:<tag>][@<digest>]` structurally. Retain digest-only references. Enforce the expected root digest before upload
- Initially accept only the `OCIArtifactCoordinates` entry. Preserve other entries. No implicit wrapping of arbitrary S3/Wget/GitHub blobs as OCI artifacts
- Reuse `AddOCIArtifact` and streaming `TransferOCIArtifact`. Buffered upload currently requires a tag: remove that limitation for digest-only and tag-plus-digest targets

### Helm API

Suggested package: `bindings/go/helm/spec/coordinates/v1alpha1`.

```go
type Coordinates struct {
    Type    runtime.Type `json:"type"`
    Name    string       `json:"name"`
    Version string       `json:"version"`
}
```

Wire type: **`HelmChartCoordinates/v1alpha1`**. `name` and `version` are required and come from the downloaded archive's `Chart.yaml`. Requested access values and the OCM resource version are not authoritative. The uploader verifies that the archive metadata exactly matches the coordinates. Source repository URLs and provenance do not belong in the coordinates.

`GetHelmChart` attaches the coordinates before access metadata is lost. They are compatible with either a native chart `.tgz` or a Helm OCI layout from which exactly one chart layer can be extracted. The OCI layout may also contain one provenance layer.

#### Helm → OCI Derivation

Conversion preserves Helm coordinates and adds OCI coordinates:

```text
GetHelmChart
  → HelmChartCoordinates
  → ConvertHelmToOCI
  → HelmChartCoordinates + OCIArtifactCoordinates
  → AddLocalResource / AddOCIArtifact
```

| OCI value | Derivation |
| ---------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| Repository | Source `helmRepository` URL path + resolved chart name. Remove only endpoint and boundary slashes. Preserve current repository-path behavior. |
| Tag | Resolved chart version, with Helm's OCI mapping of `+` to `_`. Apply consistently to layout tagging and target reference. |
| Digest | Root manifest digest returned by `CopyChartToOCILayout`. |

Example: `https://charts.example/team/charts` + chart `payments`, version `1.4.0+build.1` → Helm coordinates `payments:1.4.0+build.1` and OCI coordinates `team/charts/payments:1.4.0_build.1`.

Reject invalid OCI repository names. Do not silently lowercase/slugify. Preserve provenance inside the OCI layout. Never label a raw `.tgz` as an OCI layout.

#### JFrog Helm Upload Config

Classic Helm repositories have no standard upload API. This config selects the JFrog Artifactory implementation while keeping chart identity in `HelmChartCoordinates`.

Suggested package: `bindings/go/jfrog/helm/spec/config/upload/v1alpha1`.

```go
type Config struct {
    Type               runtime.Type `json:"type"`
    BaseURL            string       `json:"baseUrl"`
    Repository         string       `json:"repository"`
    ReindexAfterUpload bool         `json:"reindexAfterUpload,omitempty"`
}
```

```yaml
type: jfrog.helm.upload.config.ocm.software/v1alpha1
baseUrl: https://acme.jfrog.io
repository: helm-local
reindexAfterUpload: true
```

- `baseUrl`: required absolute HTTP(S) JFrog server URL. Do not include `/artifactory`. Reject userinfo, query, fragment, and non-root paths
- `repository`: required JFrog local or federated Helm repository key. Treat it as one path segment. Reject separators and traversal
- HTTP timeouts, TLS, proxy, and retry behavior come from `http.config.ocm.software`. Credentials remain outside this config
- Preserve the OCM v1 credential consumer identity: type `JFrogHelm`, hostname, optional port, and repository. Support bearer-token and username/password credentials through the standard resolver
- Upload the chart archive with `PUT <baseUrl>/artifactory/<repository>/<name>-<version>.tgz`. URL-escape segments structurally. Send the verified SHA-256 through `X-Checksum-Sha256` and disable checksum-only deployment. Do not copy the OCI manifest digest
- If requested, reindex with `POST <baseUrl>/artifactory/api/helm/<repository>/reindex` after successful upload. Reindex failure fails the transformation but cannot roll back the uploaded chart
- Construct the returned `Helm/v1` access from validated config and chart coordinates rather than trusting response-provided URLs:

  ```yaml
  type: Helm/v1
  helmRepository: https://acme.jfrog.io/artifactory/api/helm/helm-local
  helmChart: payments:1.4.0+build.1
  ```

The direct path uploads a native `.tgz` unchanged. For a Helm OCI layout, an explicit `ExtractHelmChartFromOCI` conversion selects the Helm chart layer by media type and verifies `Chart.yaml`. JFrog receives only the chart `.tgz`. A provenance layer is not published by this API path. The returned resource digest is therefore the chart archive digest, not the OCI manifest digest. Remove the consumed Helm coordinates because the returned access preserves them, and remove now-invalid OCI coordinates.

The implementation should port the behavior of the [OCM v1 JFrog Helm uploader](https://github.com/open-component-model/ocm/tree/main/cmds/jfrogplugin/uploaders/helm), not depend on its legacy uploader-plugin protocol. A dedicated JFrog transformation/backend returns the standard `Helm/v1` access. Generic Helm `ResourceRepository.UploadResource` remains unsupported.

### S3 API

Suggested package: `bindings/go/s3/spec/coordinates/v1alpha1`.

```go
type Coordinates struct {
    Type      runtime.Type `json:"type"`
    ObjectKey string       `json:"objectKey"`
}
```

```yaml
- type: S3ObjectCoordinates/v1alpha1
  objectKey: releases/payments/1.4.0/app.jar
```

- `objectKey`: required, original key unchanged. Treat as an opaque S3 key, not a filesystem path or URL. Do not unescape or `path.Clean` it
- Omit `bucketName`, `region`, `endpoint`, and `usePathStyle`: destination infrastructure belongs in config
- Omit source `version`: bucket-assigned S3 version IDs cannot be reused on upload. They are not portable artifact versions
- Removing original buckets can collapse names. Detect target collisions. Do not silently add source buckets as prefixes

#### S3 Upload Config (Backend Required)

Suggested package: `bindings/go/s3/spec/config/upload/v1alpha1`.

```go
type Config struct {
    Type         runtime.Type `json:"type"`
    BucketName   string       `json:"bucketName"`
    Region       string       `json:"region"`
    Endpoint     string       `json:"endpoint,omitempty"`
    UsePathStyle bool         `json:"usePathStyle,omitempty"`
    KeyPrefix    string       `json:"keyPrefix,omitempty"`
}
```

```yaml
type: s3.upload.config.ocm.software/v1alpha1
bucketName: delivery
region: eu-central-1
endpoint: https://s3.internal
usePathStyle: true
keyPrefix: mirror/
```

| Field | Contract |
| -------------- | -------------------------------------------------------------------------------------------------------- |
| `bucketName`   | Required existing destination bucket.                                                                    |
| `region` | Required explicit SDK signing region, including for custom endpoints. No source-region inheritance. |
| `endpoint`     | Optional absolute HTTP(S) endpoint, defaulting to AWS. No userinfo, query, fragment, or object-key path. |
| `usePathStyle` | Optional, default `false`.                                                                               |
| `keyPrefix`    | Optional exact string prefix, empty by default. Nonempty values must end in `/`.                         |

Target key = **literal `keyPrefix + coordinates.objectKey`**. Never filesystem-normalize it. Prefix containment is byte-prefix containment. S3 keys are not filesystem paths.

Upload exact bytes with existing media type. Construct `S3/v2` access from config + target key. Set `version` only from the successful destination response, omitting absent/`null` versions. Never copy source version IDs or use ETag as the resource digest. Resolve credentials from destination access.

Initially consume only the `S3ObjectCoordinates` entry and preserve other entries. Ship this config with an S3 Add transformation and implementation of the currently unsupported `UploadResource`. If execution is unsupported, reject the config at loading, before controller allowlist filtering.

### Wget API

Suggested package: `bindings/go/wget/spec/coordinates/v1alpha1`.

```go
type Coordinates struct {
    Type runtime.Type `json:"type"`
    Path string       `json:"path"`
}
```

```yaml
- type: HTTPResourceCoordinates/v1alpha1
  path: /releases/payments%20cli.tar.gz
```

- `path`: required field containing the original request URL's **escaped, root-relative URL path**, including leading `/`. Empty is valid for an empty source path. Not a filesystem path or complete URL
- Derive from `URL.EscapedPath()` before redirects. Preserve percent escapes. Do not decode, normalize, or use a signed redirect URL
- Drop authority, scheme, userinfo, query, fragment, headers, method, body, and redirect policy. These describe the source request, not artifact placement
- Intentionally **partial naming coordinates**: query-/body-/header-selected resources can share a path. They cannot authorize HTTP re-upload or uniquely identify downloaded bytes
- **No `wget.upload` config.** WebDAV, HTTP PUT, Artifactory, etc. require a specific upload protocol and mapping policy. Do not infer one from a download URL

### GitHub API

Suggested package: `bindings/go/github/spec/coordinates/v1alpha1`.

```go
type Coordinates struct {
    Type       runtime.Type `json:"type"`
    Owner      string       `json:"owner"`
    Repository string       `json:"repository"`
    Commit     string       `json:"commit"`
    Ref        string       `json:"ref,omitempty"`
}
```

```yaml
- type: GitHubSourceCoordinates/v1alpha1
  owner: acme
  repository: payments
  commit: 0123456789abcdef0123456789abcdef01234567
  ref: refs/heads/main
```

- `owner`, `repository`: required single URL path segments. Reuse repository parsing. Strip terminal `.git`, not arbitrary path prefixes
- `commit`: required full lowercase 40-hex SHA identifying the downloaded tarball's commit. `ref` is optional and informational
- Omit `repoUrl`, `apiHostname`, credentials, and archive redirect URLs
- Transfer already rejects ref-only accesses in `transfer/internal/github.go`. Preserve that requirement. Do not resolve a branch again merely to construct coordinates. Direct transformation calls must also avoid coordinate entries with unknown commits
- **No `github.upload` config.** Git replication and release-asset publication are different operations/access models. Neither is the inverse of this downloader

### LocalBlob, OCI Layers, and Files

- `LocalBlob`: preserve the existing coordinate list, even with `globalAccess`. No `LocalBlobArtifactCoordinates` or extra local-upload config. Existing transfer settings and component target control local storage
- Legacy OCI layout: parse `referenceName` as a **relative** OCI repository/tag only when no OCI-coordinate family entry exists. A malformed or unsupported OCI-coordinate entry is an error, not permission to fall back. Other-family entries do not disable the legacy OCI fallback. Never treat arbitrary tarballs as OCI layouts
- `OCIImageLayer`: registered, but unsupported as a standalone resource by the current transfer planner/resource adapter. Commonly used as `LocalBlob.globalAccess`. Do not reinterpret it as `OCIImage` or replace existing coordinates with container repository coordinates
- `File`: carries staging URI/media type/digest between transformations. Never persist temporary paths as artifact coordinates
- `File`, `Dir`, `UTF8` constructor inputs do not imply external coordinates

### Configuration and Execution

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    copyMode: allResources
  - type: oci.upload.config.ocm.software/v1alpha1
    baseUrl: registry.internal/mirror
```

```text
copyMode permits copying?
  → derive coordinates from access/conversion, or read local-blob coordinates
  → select compatible configured uploader and its coordinate-family entry
  → generate and validate target access
  → upload; use returned resource in the descriptor
```

- One effective config per uploader kind. After generic flattening, later complete entries replace earlier ones **as a whole**. Validate each entry. Nil/missing means no policy
  Upload policy selects the uploader before coordinate-family lookup. List order never resolves ambiguity. Future matchers may use resource identity or metadata. If a Helm OCI layout matches both JFrog Helm and OCI configs, require explicit policy rather than implicit format preference
- Explicit compatible uploader overrides `uploadType` / `--upload-as`. Unmatched resources retain existing placement. Selected-upload failures are errors, never fallback signals
- Resource destinations are independent of the component repository: OCI resource upload may accompany a CTF component target
- Credentials and HTTP settings retain their existing config types. No implicit field merging, matchers, user CEL, or credentials in uploader configs
- Resolve config once during planning. Generate CEL in transformation specs. Uploaders receive evaluated target access. Preserve OCI streaming
- `BuildAndCheck` checks graph/schema/expression types. Coordinate-specific and output-dependent validation must still run before upload
- Dry-run may contain expressions. Replay needs the graph and credentials/plugins, not original uploader config

### Artifact Coordinate Lifecycle and Integrity

| Transition | Action |
| ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| External → local                   | Derive or replace the current access family's coordinates. Preserve other entries that remain valid for the produced representation.                   |
| Local → local | Preserve the complete list, including unknown entries and `localBlob` with `globalAccess`. |
| Content-preserving external upload | Remove only the consumed family entry if returned access preserves its coordinates. Preserve other valid entries.                                      |
| Content conversion | Preserve, translate, or remove each entry according to compatibility with the resulting representation. Never carry known-invalid coordinates forward. |
| Failed upload | Leave source coordinate list and descriptor unchanged. |
| Subsequent download | Regenerate the current access family's entry without discarding valid alternatives. |
| Empty list | Remove the reserved label. |

- Cleanup runs **after backend success**, on a copied output resource. Remove only the consumed entry. Remove the label only when its list becomes empty. Preserve unrelated labels, compatible coordinates, and shared source branches. Later descriptor-publication failure may leave uploaded artifacts. Transfer is not transactional
- Artifact coordinates are transport metadata, not provenance. OCI, S3, and JFrog prefixes become part of new accesses. Subsequent downloads do not guess them away
- Helm OCI layout → JFrog Helm is an explicit representation conversion. Set the returned resource digest to the extracted chart archive digest, remove incompatible OCI coordinates, and do not claim provenance preservation
- Unsigned coordinate changes preserve descriptor normalization **if other signing-relevant fields remain unchanged**. Reject automatic mutation of signing-relevant coordinate labels
- Verify resource digests independently. Helm → OCI conversion is not automatically signature-preserving. Component signatures and OCI artifact signatures are separate
- Reject endpoint overrides, namespace escapes, and conflicting target coordinates within a transfer. Do not assume identical content when equality cannot be established. Apply technology-specific path/key semantics above

### Implementation Scope

| Area | Required work / existing constraint |
| ---------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `descriptor/coordinates` (new)                                                                                   | `List` plus technology-neutral label decode/encode/find/upsert/remove helpers, copying, duplicate-family/signing guards. Family key is `runtime.Type.Name`. No technology imports.                                                                    |
| `<technology>/spec/coordinates/v1alpha1` (new)                                                                   | Register and validate the five payloads. Generate schemas, deepcopy, and type methods.                                                                                                                                                                |
| `<technology>/spec/config/upload/v1alpha1` (new)                                                                 | OCI first, then S3 with its backend and JFrog Helm with explicit OCI-layout extraction. Reuse [generic filtering](../../bindings/go/configuration/generic/v1/spec/filter.go), scheme conversion, and flattened priority.                              |
| Technology transformations                                                                                       | Produce coordinates before access/resolved metadata is lost. Copy output resources.                                                                                                                                                                   |
| JFrog Helm backend (new)                                                                                         | Port OCM v1 PUT/checksum/auth/reindex behavior. Reuse HTTP config. Return `Helm/v1`. Do not make generic Helm uploadable.                                                                                                                             |
| [Transfer planner](../../bindings/go/transfer/internal/graph.go)                                                 | Separate upload-policy input. Existing `BuildGraphDefinition` remains the no-policy wrapper. CLI/controller share the compiler.                                                                                                                       |
| [Transformations](../../bindings/go/transform/graph/builder/builder.go) | Reuse CEL/DAG execution, `AddOCIArtifact`, `TransferOCIArtifact`, and `ResourceRepository.UploadResource`. No new execution registry. |
| [Controller config](../../bindings/go/kubernetes/controller/pkg/configuration/config.go)                         | Explicitly allowlist implemented uploader types. Otherwise they are dropped before hashing.                                                                                                                                                           |
| [Replication](../../bindings/go/kubernetes/controller/internal/controller/replication/replication_controller.go) | Replace source-digest-only skipping with a fingerprint of successful source identity/digest + effective policy + target. Load config before skipping. Watch Secret/ConfigMap changes. Persist fingerprint only after descriptor publication succeeds. |
| [OCI backend](../../bindings/go/oci/repository.go)                                                               | Align buffered digest-only/tag-plus-digest upload with streaming. Never invent tags or drop pins.                                                                                                                                                     |

**First increment:** OCI → coordinate-bearing local blob → configured OCI upload, with Helm-to-OCI coordinates and streaming parity. Add S3 and JFrog Helm upload support as separate backend increments.

**Constructor integration is separate:** S3/Wget inputs return only `ProcessedBlobData`. `constructor/construct.go` ignores `ProcessedResource` when blob data is also present. Metadata propagation must be explicit before claiming constructor-produced blobs automatically carry artifact coordinates.

#### Acceptance Tests

- Golden JSON/YAML, list decode/encode, multi-family round trip, duplicate-family/version rejection, order-independent selection, strict validation, canonical new type versions, config last-wins behavior, ambiguous-uploader errors, capability errors, allowlisting
- OCI host:port removal, relative paths, tag/digest variants, root-digest verification, offline round trips, buffered/streaming parity
- Helm resolved metadata, repository paths, `+` → `_`, provenance, both coordinate families attached after conversion
- JFrog native-chart and OCI-layout inputs, metadata mismatch, path escaping, checksum header, auth, response validation, optional reindex, returned Helm access, digest replacement, provenance loss, retry behavior
- S3 opaque keys/prefixes, bucket/version omission, destination version capture, media type, cross-bucket collisions
- Wget escapes, empty path, request/query omission, redirects, no inferred destination
- GitHub `.git` normalization, pinned commit versus ref, enterprise host removal, ref-only rejection
- Local hops, legacy fallback with non-OCI entries, unknown/invalid coordinates, conversion invalidation, per-entry cleanup, empty-label removal, failure/retry/multi-target isolation, signing invariance, unrelated metadata
- Config-only controller retransfer and CLI/controller graph parity

Publish label/config schemas and examples with implementation. Do not advertise unsupported upload configs or reverse conversions.
