---
title: "Transfer Configuration"
description: "Complete reference for OCM transfer configuration: transfer settings, uploader configurations, resource matching, the HTTP streaming uploader, CEL target URLs, schema, and field descriptions."
icon: "🚚"
weight: 7
toc: true
---

This page is the technical reference for OCM transfer configuration. For a
task-oriented walkthrough of routing resources to a custom upload target, see the
[Configure Custom Uploads During Transfer]({{< relref "docs/tutorials/configure-custom-uploads.md" >}})
tutorial. For the conceptual model, see
[Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}}).

## Configuration Types

Transfer behaviour is controlled by two configuration types embedded in the
standard OCM configuration file. Both are carried as entries inside the central
`generic.config.ocm.software/v1` configuration and may appear together:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    copyMode: allResources
    uploadType: ociArtifact
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1
    targetURL: '${"https://mytarget.registry.com/uploads" + url(resource.access.url).path}'
    method: PUT
```

| Type                                                        | Purpose                                                                            |
|-------------------------------------------------------------|------------------------------------------------------------------------------------|
| `transfer.config.ocm.software/v1alpha1`                     | Global transfer settings: recursion, which resources are copied and how.           |
| `http.uploader.transfer.config.ocm.software/v1alpha1`       | Per-match rule that streams a resource to a custom HTTP target.                    |
| `jfrog.helm.uploader.transfer.config.ocm.software/v1alpha1` | Per-match rule that deploys a Helm chart into a JFrog Artifactory Helm repository. |

By default the CLI looks for configuration in `$HOME/.ocmconfig`. Pass
`--config <file>` to use a different file. The corresponding CLI flags
(`--recursive`, `--copy-resources`, `--upload-as`) override the transfer config
when set.

## Transfer Settings

The `transfer.config.ocm.software/v1alpha1` type controls the global transfer
behaviour. All fields are optional; when omitted they resolve to their defaults.

### Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/Config.schema.json" >}}

### Fields

| Field        | Type        | Default            | Description                                                                     |
|--------------|-------------|--------------------|---------------------------------------------------------------------------------|
| `recursive`  | `-1` or `0` | `0` (no recursion) | `-1` transfers the whole reference tree; `0` transfers only the named version.  |
| `copyMode`   | enum        | `localBlob`        | Which resources are copied. See [Copy Mode](#copy-mode).                        |
| `uploadType` | enum        | `localBlob`        | How copied resources are stored in the target. See [Upload Type](#upload-type). |

#### Copy Mode

| Value          | Meaning                                                                                                               |
|----------------|-----------------------------------------------------------------------------------------------------------------------|
| `localBlob`    | Copy only resources already stored as local blobs. External resources keep their original access and are not fetched. |
| `allResources` | Fetch every external resource and re-upload it to the target. Equivalent to the CLI `--copy-resources` flag.          |

#### Upload Type

Only relevant when a resource is being copied.

| Value         | Meaning                                                                                                     |
|---------------|-------------------------------------------------------------------------------------------------------------|
| `localBlob`   | Embed the resource content as a local blob in the target component version's manifest. This is the default. |
| `ociArtifact` | Upload the resource as a separate OCI artifact in the target registry (OCI targets only; `--upload-as`).    |

## Uploader Configurations

An **uploader configuration** streams resources that match a rule to a custom
target instead of the default download-and-embed path. The config type dedicates
each uploader to a specific target:
`http.uploader.transfer.config.ocm.software/v1alpha1` streams to an HTTP endpoint;
`jfrog.helm.uploader.transfer.config.ocm.software/v1alpha1` deploys a Helm chart
into a JFrog Artifactory Helm repository. Each entry is an independent rule; you
may declare several.

During transfer, the **first** uploader whose `match` applies to a resource wins,
and it takes precedence over `copyMode`/`uploadType` for that resource. Because
matching is first-match, declare more specific rules before broader ones.

### `http.uploader.transfer.config.ocm.software/v1alpha1`

Streams a matched resource's content directly to an HTTP endpoint (typically a
`PUT` upload) and rewrites the resource to a `Wget/v1` access pointing at the
uploaded location. The **source** may be any access type (wget, OCI, S3, GitHub,
…) — its content is fetched through the access-type-specific downloader; only the
**target** is always an HTTP endpoint. The source content is piped straight into
the request body, so it is never buffered in memory or on disk. The digest is
computed during the stream, or — when the source resource already carries one —
verified as the bytes pass through.

The upload request and the published access are kept separate. `method`, `header`,
`body` and `noRedirect` describe the **upload request** only. The **published**
`Wget/v1` access — the download access recorded on the transferred resource —
carries just the resolved `url` and `mediaType`, never the write verb, body or
request headers. This ensures a later `ocm download` issues a plain read (GET) and
cannot re-send the write request that would overwrite the uploaded object.

#### Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/HTTPUploaderConfig.schema.json" >}}

#### Fields

`targetURL` and `mediaType` also become the published `Wget/v1` download access;
`method`, `header`, `body` and `noRedirect` apply to the upload request only:

| Field                 | Type                  | Applies to                        | Description                                                                            |
|-----------------------|-----------------------|-----------------------------------|----------------------------------------------------------------------------------------|
| `match.accessType`    | `runtime.Type`        | —                                 | Access type this uploader applies to (matched by name; omitted version = any).         |
| `match.name`          | string (optional)     | —                                 | Restrict the match to resources with this exact name.                                  |
| `match.version`       | string (optional)     | —                                 | Restrict the match to resources with this exact version.                               |
| `match.extraIdentity` | `map[string]string`   | —                                 | Restrict the match to resources whose identity contains these key/value pairs.         |
| `targetURL`           | CEL expression        | request + published (`url`)       | The upload URL; also the published download URL. See CEL Expressions below.            |
| `method`              | string                | request (`verb`)                  | HTTP method for the upload request. Defaults to PUT. Not on the published access.      |
| `header`              | `map[string][]string` | request                           | HTTP headers sent with the upload request. May be CEL-templated. Request only.         |
| `noRedirect`          | bool                  | request                           | Disable following HTTP redirects on the upload. Not on the published access.           |
| `mediaType`           | string                | request + published (`mediaType`) | Media type recorded on the resource. Defaults to the source's.                         |

### `jfrog.helm.uploader.transfer.config.ocm.software/v1alpha1`

Deploys a matched Helm chart resource into a JFrog Artifactory Helm repository.
The chart archive is located in the source and streamed directly into an HTTP
`PUT` to
`<url>/artifactory/<repository>/<component>/<component-version>/<resource>-<resource-version>.tgz`
with `Content-Type: application/gzip`. A resource with an extra identity gets a
hash of it appended to the file name. The transferred resource is rewritten to a
`Helm/v1` access with `helmRepository: <url>/artifactory/api/helm/<repository>`
and `helmChart: <name>:<version>`.

The uploader does not parse the chart. Artifactory reads it when the chart is
deployed and records its name and version, which the uploader reads back
(`GET <url>/artifactory/api/storage/<repository>/<path>?properties=chart.name,chart.version`)
and publishes. If Artifactory records no chart metadata, the content is not a Helm
chart: the uploader deletes the file again and fails the transfer.

Artifactory indexes deployed charts on its own. After each upload the uploader
additionally requests an index recalculation for the uploaded chart only
(`POST <url>/artifactory/api/helm/<repository>/<path>/reindex`, available from
Artifactory 7.105.2). If that request fails, for example because the credentials
may not trigger it, the uploader logs a warning and the transfer continues. Set
`reindex: false` to skip it.

The file name is derived from the resource, not from the chart, so the target
repository must not enable **Enforce Chart Name and Version** of Artifactory's
[Helm Enforce Layout](https://docs.jfrog.com/artifactory/docs/kubernetes-helm-chart-repositories);
such repositories reject the upload with `403`.

The chart streams from its source straight into the upload; it is not written to
disk. Only `Helm/v1` charts whose credentials use client certificates, a custom CA
or a provenance keyring go through the Helm downloader, which buffers the chart.

#### Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/JFrogHelmUploaderConfig.schema.json" >}}

#### Fields

| Field        | Type              | Description                                                                                                                                                |
|--------------|-------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `match`      | `UploaderMatch`   | Selects resources this uploader applies to. `match.accessType` is required.                                                                                |
| `url`        | string (required) | Base URL of the Artifactory instance (scheme, host, optional port and context path) **without** the `/artifactory` segment, e.g. `https://myorg.jfrog.io`. |
| `repository` | string (required) | Artifactory Helm repository key, e.g. `helm-local`. Must be a single key — no `/`, `?` or `#`.                                                             |
| `reindex`    | bool              | Request an index recalculation for each uploaded chart. Defaults to `true`. A failing request only logs a warning.                                         |

#### Supported Sources

The access type only decides how the bytes are fetched. The chart archive is then
located in the content itself, whatever media type the resource declares:

- a packaged chart (gzip-compressed `.tgz`), uploaded as is;
- a tar containing a packaged chart (as the Helm downloader produces it, with a provenance file), whose `.tgz` is uploaded;
- a Helm chart OCI artifact (config media type `application/vnd.cncf.helm.config.v1+json`), whose chart layer (`application/vnd.cncf.helm.chart.content.v1.tar+gzip` or legacy `application/tar+gzip`) is uploaded.

Other content fails the transfer before anything is uploaded; a gzip archive that
is not a chart is rejected after the upload, as described above.

| Source access type | How the bytes are fetched                                                                                                                                  |
|--------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `Helm/v1`          | Streamed from the chart URL listed in the repository `index.yaml`; `oci://` charts are streamed from the registry. The provenance file is not transferred. |
| `OCIImage/v1`      | Streamed from the OCI registry.                                                                                                                            |
| `LocalBlob`        | Streamed from the source component version (for example a chart added with the `helm` input, stored as an OCI artifact).                                   |
| `Wget/v1`          | Streamed from the URL. Checksums advertised by the source server are not checked; the source digest is (see below).                                        |
| Other remote types | Downloaded with the resource repository of the access type, for example `S3/v1`.                                                                           |

#### Digest

A `genericBlobDigest/v1` SHA-256 source digest is sent with the upload as
`X-Checksum-Sha256`, so Artifactory rejects the upload if the streamed bytes do not
match and never stores them; the uploader verifies it as well. Before uploading, the
uploader asks Artifactory to deploy the chart by that checksum
(`X-Checksum-Deploy: true`); if Artifactory already stores the same content, the
chart is not uploaded again. The same applies to charts extracted from an OCI
artifact, whose chart layer digest is known up front. A chart extracted from an OCI
artifact gets the SHA-256 of the uploaded `.tgz` instead of its source digest (for example an `ociArtifactDigest/v1`), so
signatures over the old digest do not carry over.

#### Credentials

Upload credentials are resolved for the `HelmChartRepository` consumer identity of
the Artifactory Helm API (`<url>/artifactory/api/helm/<repository>`), falling back to
the `Wget` consumer identity of the upload URL. Configure either one:

```yaml
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: HelmChartRepository
          hostname: myorg.jfrog.io
        credentials:
          - type: HelmHTTPCredentials/v1
            username: <USERNAME>
            password: <PASSWORD>
```

```yaml
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: Wget
          hostname: myorg.jfrog.io
        credentials:
          - type: WgetCredentials/v1
            username: <USERNAME>
            password: <PASSWORD>
```

When both match, the `HelmChartRepository` credentials win. An Artifactory access
token can be used as `password`; with `WgetCredentials/v1` it can also be set as
`identityToken` (sent as a bearer token). `HelmHTTPCredentials/v1` client
certificates (`certFile`/`keyFile`) are not supported for uploads; use the
`certificate` and `privateKey` of `WgetCredentials/v1` for mutual TLS. See
[`HelmHTTPCredentials/v1`]({{< relref "docs/reference/credential-types.md#helmhttpcredentialsv1" >}}),
[`WgetCredentials/v1`]({{< relref "docs/reference/credential-types.md#wgetcredentialsv1" >}}) and
[Credential Consumer Identities]({{< relref "docs/reference/credential-consumer-identities.md" >}}).

#### Example

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: jfrog.helm.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Helm/v1
    url: https://myorg.jfrog.io
    repository: helm-local
```

### Routing Resources to Different Targets

Because a rule can match on identity as well as access type, several resources of
the **same** access type can be routed to **different** targets. List the specific
rules first; a final rule without `name`/`version`/`extraIdentity` acts as a catch-all:

```yaml
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    copyMode: allResources
  # Docs go to the docs bucket.
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1
      name: docs
    targetURL: '${"https://docs.example.com" + url(resource.access.url).path}'
  # Everything else Wget goes to the generic bucket.
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1
    targetURL: '${"https://blobs.example.com/" + resource.name + "/" + resource.version}'
```

### CEL Expressions

`targetURL` and every `header` value are [CEL](https://cel.dev/) expressions — the
same expression language the transfer graph uses to resolve every other field. A
CEL value **must be wrapped in `${…}`**, matching how every other CEL field is
written in the transfer graph. It is evaluated against the source resource, exposed
under the `resource` alias, and resolved by the transfer runtime, so the produced
plan is deterministic. CEL string concatenation (`+`), conditionals (`cond ? a : b`),
and comparisons are all available. Append any static query string inside the
expression. (A `header` value with no `${…}` is a plain literal and is sent verbatim.)

The `resource` alias always exposes:

| Expression                               | Value                                                        |
|------------------------------------------|--------------------------------------------------------------|
| `resource.name`                          | Resource name.                                               |
| `resource.version`                       | Resource version.                                            |
| `resource.type`                          | Resource type.                                               |
| `resource.extraIdentity.<key>`           | A value from the resource's extra identity.                  |
| `resource.labels`                        | The resource labels as a list of `{name, value}` objects.    |
| `resource.digest.value`                  | The source digest value, when the resource carries a digest. |
| `resource.digest.hashAlgorithm`          | The source digest hash algorithm (e.g. `SHA-256`).           |
| `resource.digest.normalisationAlgorithm` | The source digest normalisation algorithm.                   |

Every field of the **source access** is exposed dynamically under
`resource.access.<field>`, so the expression works with any access type — the
field names are exactly those of that access. For example:

| Source access | Available under `resource.access` |
|---------------|-----------------------------------|
| `Wget/v1`     | `url`, `mediaType`                |
| `OCIImage/v1` | `imageReference`                  |
| `S3/v1`       | `bucket`, `key`, `region`, …      |

To decompose a URL-bearing access into its parts, use the inbuilt `url()` CEL
function (CEL has no URL parser). `url(<string>)` (also callable as
`<string>.url()`) parses a URL string and returns a map with the string keys
`scheme`, `host`, `hostname`, `port`, `path`, `rawPath`, `rawQuery`, `fragment`,
and `user`. For a `Wget/v1` source, `url(resource.access.url).path` yields the
source URL's path. Because the field set is derived from the matched resource's
own access, an expression may only reference fields that exist on every resource
the uploader matches — scope the rule with `match.accessType` so all matched
resources share a shape.

Further inbuilt functions help build checksum headers:
`contentDigestAlgorithm(<string>)` maps an OCM digest algorithm name to its RFC 9530
`Content-Digest`/`Repr-Digest` key (`SHA-256` → `sha-256`); `hex.decode`/`hex.encode`
and the CEL encoders extension's `base64.encode`/`base64.decode` convert a hex digest
to the base64 value those fields expect. See [Templating Headers](#templating-headers).

Examples:

```yaml
# Preserve the source path under a new host
targetURL: '${"https://mytarget.example.com" + url(resource.access.url).path}'

# Route by name and version
targetURL: '${"https://cdn.example.com/" + resource.name + "/" + resource.version + "/blob"}'

# Use extra identity attributes
targetURL: '${"https://" + resource.extraIdentity.region + ".example.com/" + resource.extraIdentity.arch + url(resource.access.url).path}'

# Conditional target driven by an extra-identity attribute
targetURL: '${resource.extraIdentity.tier == "public" ? "https://cdn.example.com" + url(resource.access.url).path : "https://internal.example.com" + url(resource.access.url).path}'

# Non-wget source: reference an access-specific field (OCI imageReference)
targetURL: '${"https://mirror.example.com/" + resource.access.imageReference}'
```

Referencing a field that is absent at execution time fails the transfer with a
clear error rather than producing a partial URL.

#### Templating Headers

`header` values are templated with the same `${…}` CEL expressions. This is how
you forward a checksum the source already advertised on the upload request. Header
expressions read `resource.digest`, which is only present when the source resource
carries a digest (e.g. pinned from the source via the checksum-http configuration),
so scope the rule with `match` so every matched resource has one. A value without
`${…}` is sent as a literal.

`resource.digest.value` is the **hex** digest and `resource.digest.hashAlgorithm`
is the OCM algorithm name (e.g. `SHA-256`). Two inbuilt CEL functions build a
strictly conformant
[`Content-Digest`](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Digest)
/ `Repr-Digest` field (RFC 9530):

- `contentDigestAlgorithm(<name>)` maps the OCM algorithm name to the RFC 9530 key,
  lower-cased and canonicalized (`SHA-256` → `sha-256`, `SHA-512` → `sha-512`,
  `MD5` → `md5`, and `SHA-1` → the registered key `sha`). Matching is case- and
  separator-insensitive; an unknown algorithm fails the transfer.
- RFC 9530 carries the digest **value** as base64, while `resource.digest.value` is
  hex, so convert it with `base64.encode(hex.decode(resource.digest.value))`. The
  `base64.encode`/`base64.decode` functions come from the CEL
  [encoders extension](https://github.com/google/cel-go/blob/master/ext/README.md);
  `hex.encode`/`hex.decode` are inbuilt companions.

```yaml
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1
    targetURL: '${"https://mytarget.example.com/uploads" + url(resource.access.url).path}'
    method: PUT
    header:
      # RFC 9530 Content-Digest: sha-256=:<base64>:
      Content-Digest: ['${contentDigestAlgorithm(resource.digest.hashAlgorithm) + "=:" + base64.encode(hex.decode(resource.digest.value)) + ":"}']
      # A simple non-standard checksum header carrying the raw hex value.
      X-Checksum-Sha256: ['${resource.digest.value}']
      # A static literal header is sent verbatim (no ${...}).
      X-Uploaded-By: ['ocm-transfer']
```

##### Example: JFrog Artifactory

[Artifactory](https://jfrog.com/help/r/jfrog-artifactory-documentation) verifies
uploads against client-supplied checksum headers and takes the **hex** digest
directly (no RFC 9530 base64), so `resource.digest.value` maps straight onto its
`X-Checksum-*` family. With the default checksum policy Artifactory requires a
checksum header on deploy:

```yaml
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1
    # Deploy under <repo>/<path>; here the source URL path is reused.
    targetURL: '${"https://myorg.jfrog.io/artifactory/my-repo" + url(resource.access.url).path}'
    method: PUT
    header:
      # Artifactory verifies the uploaded bytes against this hex SHA-256.
      X-Checksum-Sha256: ['${resource.digest.value}']
```

To link an artifact that Artifactory **already** stores without re-uploading the
body (["Deploy Artifact by Checksum"](https://jfrog.com/help/r/jfrog-rest-apis/deploy-artifact-by-checksum)),
set `X-Checksum-Deploy: true` alongside the checksum. Artifactory returns `201` when
it finds a matching artifact and `404` when the content must still be uploaded:

```yaml
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1
    targetURL: '${"https://myorg.jfrog.io/artifactory/my-repo" + url(resource.access.url).path}'
    method: PUT
    header:
      X-Checksum-Deploy: ['true']
      X-Checksum-Sha256: ['${resource.digest.value}']
```

Both require the source resource to carry a SHA-256 digest (`resource.digest.hashAlgorithm == "SHA-256"`).
To deploy Helm charts into an Artifactory Helm repository and publish a `Helm/v1`
access, use
[`jfrog.helm.uploader.transfer.config.ocm.software/v1alpha1`]({{< relref "docs/reference/transfer-configuration.md" >}}#jfroghelmuploadertransferconfigocmsoftwarev1alpha1)
instead.

#### Credentials

The uploader resolves credentials for the **target** URL independently from the
source resource, using the target host's
[consumer identity]({{< relref "docs/reference/credential-consumer-identities.md" >}})
(the same `Wget` identity used for downloads). Configure target-side credentials
the same way you would for any HTTP endpoint; see
[Credential Types]({{< relref "docs/reference/credential-types.md" >}}).

## Notes

### Precedence

For a given resource, an uploader match takes precedence over the default handlers
and runs regardless of `copyMode`. A resource with no matching uploader follows the
normal `copyMode`/`uploadType` behaviour.

### Deterministic Plans

Transfer produces a deterministic transformation plan: components are processed in
sorted order and transformation identifiers are derived from stable hashes. The
plan is rendered with human-readable labels such as
`my-app@1.0.0 [Stream icons to mytarget.registry.com]`.

## Related Documentation

- [Configure Custom Uploads During Transfer]({{< relref "docs/tutorials/configure-custom-uploads.md" >}}) — tutorial that walks through an uploader end to end
- [Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}}) — the conceptual transfer model
- [Working with HTTP Resources]({{< relref "docs/tutorials/wget-http-resources.md" >}}) — the `Wget/v1` type produced by the HTTP streaming uploader
- [HTTP Client Configuration]({{< relref "docs/reference/http-client-configuration.md" >}}) — tuning the HTTP client used for the upload
