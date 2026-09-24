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

| Type                                                  | Purpose                                                                  |
|-------------------------------------------------------|--------------------------------------------------------------------------|
| `transfer.config.ocm.software/v1alpha1`               | Global transfer settings: recursion, which resources are copied and how. |
| `http.uploader.transfer.config.ocm.software/v1alpha1` | Per-match rule that streams a resource to a custom HTTP target.          |

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
each uploader to a specific target: `http.uploader.transfer.config.ocm.software/v1alpha1`
streams to an HTTP endpoint. Each entry is an independent rule; you may declare
several.

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

#### Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/HTTPUploaderConfig.schema.json" >}}

#### Fields

The request fields map field-for-field onto the resulting
[`Wget/v1`]({{< relref "docs/reference/input-and-access-types.md" >}}) access:

| Field                 | Type                  | Maps to (`Wget/v1`) | Description                                                                                        |
|-----------------------|-----------------------|---------------------|----------------------------------------------------------------------------------------------------|
| `match.accessType`    | `runtime.Type`        | —                   | Access type this uploader applies to (matched by name; omitted version = any).                     |
| `match.name`          | string (optional)     | —                   | Restrict the match to resources with this exact name.                                              |
| `match.version`       | string (optional)     | —                   | Restrict the match to resources with this exact version.                                           |
| `match.extraIdentity` | `map[string]string`   | —                   | Restrict the match to resources whose identity contains these key/value pairs.                     |
| `targetURL`           | CEL expression        | `url`               | The upload URL. See CEL Expressions below.                                                         |
| `method`              | string                | `verb`              | HTTP method for the upload request. Defaults to PUT.                                               |
| `header`              | `map[string][]string` | `header`            | HTTP headers to send with the upload request. Values may be CEL-templated; see Templating Headers. |
| `noRedirect`          | bool                  | `noRedirect`        | Disable following HTTP redirects.                                                                  |
| `mediaType`           | string                | `mediaType`         | Media type recorded on the resource. Defaults to the source's.                                     |

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
