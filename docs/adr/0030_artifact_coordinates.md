# ADR: Portable Artifact Coordinates and Typed Upload Configuration

* **Status**: proposed
* **Deciders**: SIG Runtime, Fabian Burth (@fabianburth)
* **Date**: 2026-09-18

## Problem

Converting external access to `localBlob` loses upload coordinates. The receiving environment knows the destination endpoint, but cannot infer artifact coordinates from OCM resource name/version.

Refines [ADR-0018](../../docs/adr/0018_transfer_configuration.md); replaces this draft's earlier user-facing template proposal.

## Decision

```text
artifact coordinates          + uploader config       = target access
acme/payments:1.4.0              registry.internal/mirror
                                ↓
registry.internal/mirror/acme/payments:1.4.0
```

| Concern | Decision |
| --- | --- |
| Portable coordinates | List of typed, versioned artifact coordinates in one resource label. No endpoints or credentials. |
| Destination policy | Flat OCM config type per uploader kind. |
| User interface | Destination settings; no CEL or templates initially. |
| Execution | Planner generates CEL; registered transformations perform uploads. |
| Coordinate cleanup | Remove after successful external upload **only if returned access preserves the required coordinates**. |

The following names, fields, and derivation rules define the proposed API. Package locations are implementation suggestions.

## Access-Type Coverage

**A downloader needs an artifact-coordinate contract; it does not necessarily have an inverse uploader.**

| Access | Downloaded representation | Coordinate-list entry | Upload configuration |
| --- | --- | --- | --- |
| `OCIImage/v1` | OCI layout containing the selected artifact graph | `OCIArtifactCoordinates/v1alpha1` | `oci.upload.config.ocm.software/v1alpha1` |
| `Helm/v1` | Native chart `.tgz` + optional provenance; optionally converted to OCI layout | `HelmChartCoordinates/v1alpha1`; add `OCIArtifactCoordinates/v1alpha1` after conversion | `jfrog.helm.upload.config.ocm.software/v1alpha1` for classic JFrog; OCI config after conversion |
| `S3/v2` | Exact object bytes | `S3ObjectCoordinates/v1alpha1` | `s3.upload.config.ocm.software/v1alpha1`; requires a new upload backend |
| `Wget/v1` | HTTP response body | `HTTPResourceCoordinates/v1alpha1` | None: download access does not specify an upload protocol |
| `GitHub/v1` | Exact commit tarball, `application/x-tgz` | `GitHubSourceCoordinates/v1alpha1` | None: a tarball cannot recreate a Git repository/commit |
| `LocalBlob/v1` | Existing stored representation | Preserve existing coordinates | Selected by coordinates; otherwise existing local storage behavior |
| `OCIImageLayer/v1` | Raw layer, not an OCI artifact | No new producer in this increment | None: no standalone transfer handler; durable upload needs a referencing manifest |
| `File/v1alpha1` | Transformation staging file | Preserve resource coordinates alongside file | None: internal blob locator |

Access aliases resolve through existing schemes. New coordinate/config types accept **only their canonical, versioned spelling**; no legacy aliases exist to support. Classic Helm repositories have no standard upload protocol; JFrog support is therefore a vendor-specific uploader, not generic `Helm/v1` upload behavior. Maven is absent from the current Go implementation and outside this API contract.

## Shared Artifact Coordinates Label

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

The same envelope can later carry Maven coordinates; Maven remains outside the current implementation.

* Exactly one reserved label. Its value is a JSON/YAML array of typed coordinate payloads. Each payload `type` owns versioning; omit `Label.Version`.
* At most one entry per target access family. The family key is `runtime.Type.Name`; versions of the same name therefore compete. Reject duplicate type names, regardless of version. Producers upsert their family entry rather than append blindly.
* List order has no precedence or selection meaning. Upload policy—including future resource matching—selects the uploader; that uploader selects its corresponding coordinate family.
* If multiple configured uploaders match a resource, fail unless configuration explicitly resolves the ambiguity. Never choose the first coordinate entry.
* Typed payloads are not partially valid access specs. No nested `access`, generic `properties`, or extra `format` field; each coordinate type fixes coordinate and representation semantics.
* Multiple coordinate entries do not imply conversion support. An entry is eligible only when the current blob representation is compatible with the selected uploader or a supported conversion produces that representation.
* Generated labels are unsigned. Reject automatic replacement/removal of signing-relevant labels.
* Known payloads: reject unknown fields and invalid values when producing or consuming. Opaque local copies preserve unknown types/versions without interpreting them.
* Resource-aware transformers attach coordinates on copied output resources. `DownloadResource` remains a blob-returning API and must not mutate input metadata.
* Blob media type stays in `File.mediaType` / `LocalBlob.mediaType`; integrity stays in `resource.digest`. Do not duplicate these in every coordinate entry.
* Construct allowlisted relative fields rather than copying/redacting entire accesses. Coordinates are untrusted data, not CEL source or authority to choose an endpoint.

## OCI API

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
| --- | --- |
| `repository` | Required, registry-relative OCI repository path. No leading/trailing slash, host, scheme, tag, or digest. Validate using OCI repository grammar. |
| `tag` | Optional valid OCI tag. Never derive from OCM resource version. |
| `digest` | Optional full OCI root digest, including algorithm. Not the digest of the layout tar archive. |

At least one of `tag`/`digest` is required. Preserve both when present. Digest pins content; tag requests publication under a name.

```text
source:      registry.example:5000/team/app:1.4.0@sha256:<digest>
coordinates: repository=team/app, tag=1.4.0, digest=sha256:<digest>
```

Parse a **full source reference** with the existing OCI parser; copy its repository/tag/digest. Do not parse a relative multi-segment repository as a full reference: that loses its first segment. A tagless/digestless source needs its selected root digest before valid coordinates can be produced; never invent `latest`.

### OCI Upload Config

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

* Required authority plus optional repository prefix. Accept explicit `http://` or `https://`; no scheme means HTTPS. Reject credentials, query, fragment, tag, digest, and missing host.
* A port belongs to the authority, not an artifact tag. Normalize a trailing slash; reject empty/internal dot segments in the repository prefix.
* Build `<baseUrl>/<coordinates.repository>[:<tag>][@<digest>]` structurally. Retain digest-only references; enforce the expected root digest before upload.
* Initially accept only the `OCIArtifactCoordinates` entry. Preserve other entries; no implicit wrapping of arbitrary S3/Wget/GitHub blobs as OCI artifacts.
* Reuse `AddOCIArtifact` and streaming `TransferOCIArtifact`. Buffered upload currently requires a tag: remove that limitation for digest-only and tag-plus-digest targets.

## Helm API

Suggested package: `bindings/go/helm/spec/coordinates/v1alpha1`.

```go
type Coordinates struct {
    Type    runtime.Type `json:"type"`
    Name    string       `json:"name"`
    Version string       `json:"version"`
}
```

Wire type: **`HelmChartCoordinates/v1alpha1`**. `name` and `version` are required and come from the downloaded archive's `Chart.yaml`; requested access values and the OCM resource version are not authoritative. The uploader verifies that the archive metadata exactly matches the coordinates. Source repository URLs and provenance do not belong in the coordinates.

`GetHelmChart` attaches the coordinates before access metadata is lost. They are compatible with either a native chart `.tgz` or a Helm OCI layout from which exactly one chart layer can be extracted. The OCI layout may also contain one provenance layer.

### Helm → OCI Derivation

Conversion preserves Helm coordinates and adds OCI coordinates:

```text
GetHelmChart
  → HelmChartCoordinates
  → ConvertHelmToOCI
  → HelmChartCoordinates + OCIArtifactCoordinates
  → AddLocalResource / AddOCIArtifact
```

| OCI value | Derivation |
| --- | --- |
| Repository | Source `helmRepository` URL path + resolved chart name; remove only endpoint and boundary slashes. Preserve current repository-path behavior. |
| Tag | Resolved chart version, with Helm's OCI mapping of `+` to `_`. Apply consistently to layout tagging and target reference. |
| Digest | Root manifest digest returned by `CopyChartToOCILayout`. |

Example: `https://charts.example/team/charts` + chart `payments`, version `1.4.0+build.1` → Helm coordinates `payments:1.4.0+build.1` and OCI coordinates `team/charts/payments:1.4.0_build.1`.

Reject invalid OCI repository names; do not silently lowercase/slugify. Preserve provenance inside the OCI layout. Never label a raw `.tgz` as an OCI layout.

### JFrog Helm Upload Config

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

* `baseUrl`: required absolute HTTP(S) JFrog server URL. Do not include `/artifactory`. Reject userinfo, query, fragment, and non-root paths.
* `repository`: required JFrog local or federated Helm repository key. Treat it as one path segment; reject separators and traversal.
* HTTP timeouts, TLS, proxy, and retry behavior come from `http.config.ocm.software`; credentials remain outside this config.
* Preserve the OCM v1 credential consumer identity: type `JFrogHelm`, hostname, optional port, and repository. Support bearer-token and username/password credentials through the standard resolver.
* Upload the chart archive with `PUT <baseUrl>/artifactory/<repository>/<name>-<version>.tgz`. URL-escape segments structurally. Send the verified SHA-256 through `X-Checksum-Sha256` and disable checksum-only deployment; do not copy the OCI manifest digest.
* If requested, reindex with `POST <baseUrl>/artifactory/api/helm/<repository>/reindex` after successful upload. Reindex failure fails the transformation but cannot roll back the uploaded chart.
* Construct the returned `Helm/v1` access from validated config and chart coordinates rather than trusting response-provided URLs:

  ```yaml
  type: Helm/v1
  helmRepository: https://acme.jfrog.io/artifactory/api/helm/helm-local
  helmChart: payments:1.4.0+build.1
  ```

The direct path uploads a native `.tgz` unchanged. For a Helm OCI layout, an explicit `ExtractHelmChartFromOCI` conversion selects the Helm chart layer by media type and verifies `Chart.yaml`. JFrog receives only the chart `.tgz`; a provenance layer is not published by this API path. The returned resource digest is therefore the chart archive digest, not the OCI manifest digest. Remove the consumed Helm coordinates because the returned access preserves them, and remove now-invalid OCI coordinates.

The implementation should port the behavior of the [OCM v1 JFrog Helm uploader](https://github.com/open-component-model/ocm/tree/main/cmds/jfrogplugin/uploaders/helm), not depend on its legacy uploader-plugin protocol. A dedicated JFrog transformation/backend returns the standard `Helm/v1` access; generic Helm `ResourceRepository.UploadResource` remains unsupported.

## S3 API

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

* `objectKey`: required, original key unchanged. Treat as an opaque S3 key, not a filesystem path or URL; do not unescape or `path.Clean` it.
* Omit `bucketName`, `region`, `endpoint`, and `usePathStyle`: destination infrastructure belongs in config.
* Omit source `version`: bucket-assigned S3 version IDs cannot be reused on upload. They are not portable artifact versions.
* Removing original buckets can collapse names. Detect target collisions; do not silently add source buckets as prefixes.

### S3 Upload Config — Backend Required

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
| --- | --- |
| `bucketName` | Required destination bucket; no bucket creation. |
| `region` | Required explicit SDK signing region, including for custom endpoints. No source-region inheritance. |
| `endpoint` | Optional absolute HTTP(S) endpoint; absent means AWS. No userinfo, query, fragment, or object-key path. |
| `usePathStyle` | Optional; default `false`. |
| `keyPrefix` | Optional exact string prefix; empty by default. Nonempty value must end in `/`. |

Target key = **literal `keyPrefix + coordinates.objectKey`**. Never filesystem-normalize it. Prefix containment is byte-prefix containment; S3 keys are not filesystem paths.

Upload exact bytes with existing media type. Construct `S3/v2` access from config + target key; set `version` only from the successful destination response, omitting absent/`null` versions. Never copy source version IDs or use ETag as the resource digest. Resolve credentials from destination access.

Initially consume only the `S3ObjectCoordinates` entry and preserve other entries. Ship this config with an S3 Add transformation and implementation of the currently unsupported `UploadResource`. If an intermediate build recognizes the config before execution support exists, reject it at config loading—including before controller allowlist filtering—rather than silently dropping it.

## Wget API

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

* `path`: required field containing the original request URL's **escaped, root-relative URL path**, including leading `/`. Empty is valid for an empty source path. Not a filesystem path or complete URL.
* Derive from `URL.EscapedPath()` before redirects. Preserve percent escapes; do not decode, normalize, or use a signed redirect URL.
* Drop authority, scheme, userinfo, query, fragment, headers, method, body, and redirect policy. These describe the source request, not artifact placement.
* Intentionally **partial naming coordinates**: query-/body-/header-selected resources can share a path. They cannot authorize HTTP re-upload or uniquely identify downloaded bytes.
* **No `wget.upload` config.** WebDAV, HTTP PUT, Artifactory, etc. require a specific upload protocol and mapping policy; do not infer one from a download URL.

## GitHub API

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

* `owner`, `repository`: required single URL path segments. Reuse repository parsing; strip terminal `.git`, not arbitrary path prefixes.
* `commit`: required full 40-hex SHA, normalized lowercase; identifies the downloaded tarball's commit. `ref` is optional and informational.
* Omit `repoUrl`, `apiHostname`, credentials, and archive redirect URLs.
* Transfer already rejects ref-only accesses in `transfer/internal/github.go`. Preserve that requirement; do not resolve a branch again merely to construct coordinates. Direct transformation calls must also avoid coordinate entries with unknown commits.
* **No `github.upload` config.** Git replication and release-asset publication are different operations/access models; neither is the inverse of this downloader.

## LocalBlob, OCI Layers, and Files

* `LocalBlob`: preserve the existing coordinate list, even with `globalAccess`; no `LocalBlobArtifactCoordinates` or extra local-upload config. Existing transfer settings and component target control local storage.
* Legacy OCI layout: parse `referenceName` as a **relative** OCI repository/tag only when no OCI-coordinate family entry exists. A malformed or unsupported OCI-coordinate entry is an error, not permission to fall back. Other-family entries do not disable the legacy OCI fallback. Never treat arbitrary tarballs as OCI layouts.
* `OCIImageLayer`: registered, but unsupported as a standalone resource by the current transfer planner/resource adapter. Commonly used as `LocalBlob.globalAccess`. Do not reinterpret it as `OCIImage` or replace existing coordinates with container repository coordinates.
* `File`: carries staging URI/media type/digest between transformations. Never persist temporary paths as artifact coordinates.
* `File`, `Dir`, `UTF8` constructor inputs do not imply external coordinates.

## Configuration and Execution

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

* One effective config per uploader kind. After generic flattening, later complete entries replace earlier ones **as a whole**. Validate each entry; nil/missing means no policy.
* Matching is configuration policy, not label semantics. Future matchers may select by resource identity or metadata. Select the uploader first, then look up its declared coordinate-family name; list order never breaks ties. Multiple matching uploaders without explicit precedence are an error. In particular, a Helm OCI layout may carry both Helm and OCI coordinates; if both JFrog Helm and OCI configs are effective, policy must select one rather than relying on implicit format preference.
* Explicit compatible uploader overrides `uploadType` / `--upload-as`. Unmatched resources retain existing placement; selected-upload failures are errors, never fallback signals.
* Resource destinations are independent of the component repository: OCI resource upload may accompany a CTF component target.
* Credentials and HTTP settings retain their existing config types. No implicit field merging, matchers, user CEL, or credentials in uploader configs.
* Resolve config once during planning. Generate CEL in transformation specs; uploaders receive evaluated target access. Preserve OCI streaming.
* `BuildAndCheck` checks graph/schema/expression types; coordinate-specific and output-dependent validation must still run before upload.
* Dry-run may contain expressions; replay needs the graph and credentials/plugins, not original uploader config.

## Artifact Coordinate Lifecycle and Integrity

| Transition | Action |
| --- | --- |
| External → local | Derive or replace the current access family's coordinates; preserve other entries that remain valid for the produced representation. |
| Local → local | Preserve the complete list, including unknown entries and `localBlob` with `globalAccess`. |
| Content-preserving external upload | Remove only the consumed family entry if returned access preserves its coordinates; preserve other valid entries. |
| Content conversion | Preserve, translate, or remove each entry according to compatibility with the resulting representation. Never carry known-invalid coordinates forward. |
| Failed upload | Leave source coordinate list and descriptor unchanged. |
| Subsequent download | Regenerate the current access family's entry without discarding valid alternatives. |
| Empty list | Remove the reserved label. |

* Cleanup runs **after backend success**, on a copied output resource. Remove only the consumed entry; remove the label only when its list becomes empty. Preserve unrelated labels, compatible coordinates, and shared source branches. Later descriptor-publication failure may leave uploaded artifacts; transfer is not transactional.
* Artifact coordinates are transport metadata, not provenance. OCI, S3, and JFrog prefixes become part of new accesses; subsequent downloads do not guess them away.
* Helm OCI layout → JFrog Helm is an explicit representation conversion. Set the returned resource digest to the extracted chart archive digest, remove incompatible OCI coordinates, and do not claim provenance preservation.
* Unsigned coordinate changes preserve descriptor normalization **if other signing-relevant fields remain unchanged**. Reject automatic mutation of signing-relevant coordinate labels.
* Verify resource digests independently. Helm → OCI conversion is not automatically signature-preserving; component signatures and OCI artifact signatures are separate.
* Reject endpoint overrides, namespace escapes, and conflicting target coordinates within a transfer. Do not assume identical content when equality cannot be established. Apply technology-specific path/key semantics above.

## Implementation Scope

| Area | Required work / existing constraint |
| --- | --- |
| `descriptor/coordinates` (new) | `List` plus technology-neutral label decode/encode/find/upsert/remove helpers, copying, duplicate-family/signing guards. Family key is `runtime.Type.Name`; no technology imports. |
| `<technology>/spec/coordinates/v1alpha1` (new) | Five payloads above; registration, strict validation, generated schemas/deepcopy/type methods. |
| `<technology>/spec/config/upload/v1alpha1` (new) | OCI first; S3 with its backend; JFrog Helm with explicit OCI-layout extraction. Reuse [generic filtering](../../bindings/go/configuration/generic/v1/spec/filter.go), scheme conversion, and flattened priority. |
| Technology transformations | Produce coordinates before access/resolved metadata is lost; copy output resources. |
| JFrog Helm backend (new) | Port OCM v1 PUT/checksum/auth/reindex behavior; reuse HTTP config; return `Helm/v1`. Do not make generic Helm uploadable. |
| [Transfer planner](../../bindings/go/transfer/internal/graph.go) | Separate upload-policy input; existing `BuildGraphDefinition` remains the no-policy wrapper. CLI/controller share the compiler. |
| [Transformations](../../bindings/go/transform/graph/builder/builder.go) | Reuse CEL/DAG execution, `AddOCIArtifact`, `TransferOCIArtifact`, and `ResourceRepository.UploadResource`. No new execution registry. |
| [Controller config](../../bindings/go/kubernetes/controller/pkg/configuration/config.go) | Explicitly allowlist implemented uploader types; otherwise they are dropped before hashing. |
| [Replication](../../bindings/go/kubernetes/controller/internal/controller/replication/replication_controller.go) | Replace source-digest-only skipping with a fingerprint of successful source identity/digest + effective policy + target. Load config before skipping; watch Secret/ConfigMap changes. Persist fingerprint only after descriptor publication succeeds. |
| [OCI backend](../../bindings/go/oci/repository.go) | Align buffered digest-only/tag-plus-digest upload with streaming; never invent tags or drop pins. |

**First increment:** OCI → coordinate-bearing local blob → configured OCI upload; Helm-to-OCI coordinates; streaming parity. Add S3 and JFrog Helm upload support as separate backend increments.

**Constructor integration is separate:** S3/Wget inputs return only `ProcessedBlobData`; `constructor/construct.go` ignores `ProcessedResource` when blob data is also present. Metadata propagation must be explicit before claiming constructor-produced blobs automatically carry artifact coordinates.

### Acceptance Tests

* Golden JSON/YAML, list decode/encode, multi-family round trip, duplicate-family/version rejection, order-independent selection, strict validation, canonical new type versions, config last-wins behavior, ambiguous-uploader errors, capability errors, allowlisting.
* OCI host:port removal, relative paths, tag/digest variants, root-digest verification, offline round trips, buffered/streaming parity.
* Helm resolved metadata, repository paths, `+` → `_`, provenance, both coordinate families attached after conversion.
* JFrog native-chart and OCI-layout inputs, metadata mismatch, path escaping, checksum header, auth, response validation, optional reindex, returned Helm access, digest replacement, provenance loss, retry behavior.
* S3 opaque keys/prefixes, bucket/version omission, destination version capture, media type, cross-bucket collisions.
* Wget escapes, empty path, request/query omission, redirects, no inferred destination.
* GitHub `.git` normalization, pinned commit versus ref, enterprise host removal, ref-only rejection.
* Local hops, legacy fallback with non-OCI entries, unknown/invalid coordinates, conversion invalidation, per-entry cleanup, empty-label removal, failure/retry/multi-target isolation, signing invariance, unrelated metadata.
* Config-only controller retransfer; CLI/controller graph parity.

Publish label/config schemas and examples with implementation. Do not advertise unsupported upload configs or reverse conversions.

## Alternatives and Trade-offs

| Option | Implementation | Trade-off |
| --- | --- | --- |
| **Chosen: artifact coordinates + typed configs** | Coordinate producers, config compiler, output cleanup. | Small UI; survives offline transfer. Adds a public metadata contract. |
| Matchers + target templates | Rule matching and template compiler in transfer planning. | Flexible routing, but larger UI and cannot recover lost coordinates. |
| Extend `LocalBlob` | New access fields, schemas, specification change. | Dedicated schema location, but expands generic transport access. |
| Graph-only coordinates | Pass coordinates between nodes without persistence. | No persistent metadata, but fails disconnected/multi-hop transfers. |

**Deferred:** per-resource routing, coordinate overrides, user CEL, multiple destinations per uploader kind, arbitrary reverse conversions beyond Helm OCI layout → chart archive, general HTTP publishing, Maven API, and blob-to-OCI wrapping. Existing blobs with insufficient metadata cannot gain round-trip support automatically.
