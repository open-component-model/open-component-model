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
  - type: uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1
    stream:
      type: HTTPStreaming/v1alpha1
      targetURL: '${"https://mytarget.registry.com/uploads" + resource.access.path}'
      method: PUT
```

| Type                                             | Purpose                                                                    |
|--------------------------------------------------|----------------------------------------------------------------------------|
| `transfer.config.ocm.software/v1alpha1`          | Global transfer settings: recursion, which resources are copied and how.   |
| `uploader.transfer.config.ocm.software/v1alpha1` | Per-match rule that routes a resource through a custom upload transformer. |

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

| Value          | Meaning                                                                                                                   |
|----------------|---------------------------------------------------------------------------------------------------------------------------|
| `localBlob`    | Copy only resources already stored as local blobs. External resources keep their original access and are not fetched.     |
| `allResources` | Fetch every external resource and re-upload it to the target. Equivalent to the CLI `--copy-resources` flag.              |

#### Upload Type

Only relevant when a resource is being copied.

| Value         | Meaning                                                                                                     |
|---------------|-------------------------------------------------------------------------------------------------------------|
| `localBlob`   | Embed the resource content as a local blob in the target component version's manifest. This is the default. |
| `ociArtifact` | Upload the resource as a separate OCI artifact in the target registry (OCI targets only; `--upload-as`).    |

## Uploader Configurations

An **uploader configuration** routes resources that match a rule through a custom
upload transformer instead of the default download-and-embed path. Each
`uploader.transfer.config.ocm.software/v1alpha1` entry is an independent rule; you
may declare several.

During transfer, the **first** uploader whose `match` applies to a resource wins,
and it takes precedence over `copyMode`/`uploadType` for that resource. Because
matching is first-match, declare more specific rules before broader ones.

### Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/UploaderConfig.schema.json" >}}

### Fields

| Field                 | Type                | Description                                                                     |
|-----------------------|---------------------|---------------------------------------------------------------------------------|
| `match.accessType`    | `runtime.Type`      | Access type this uploader applies to (matched by name; omitted version = any).  |
| `match.name`          | string (optional)   | Restrict the match to resources with this exact name.                           |
| `match.extraIdentity` | `map[string]string` | Restrict the match to resources whose identity contains these key/value pairs.  |
| `stream`              | object              | Upload transformer spec, selected by its `type`. See Stream Types below.        |

### Routing Resources to Different Targets

Because a rule can match on identity as well as access type, several resources of
the **same** access type can be routed to **different** targets. List the specific
rules first; a final rule without `name`/`extraIdentity` acts as a catch-all:

```yaml
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    copyMode: allResources
  # Docs go to the docs bucket.
  - type: uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1
      name: docs
    stream:
      type: HTTPStreaming/v1alpha1
      targetURL: '${"https://docs.example.com" + resource.access.path}'
  # Everything else Wget goes to the generic bucket.
  - type: uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Wget/v1
    stream:
      type: HTTPStreaming/v1alpha1
      targetURL: '${"https://blobs.example.com/" + resource.name + "/" + resource.version}'
```

## Stream Types

The `stream` block selects and configures the transformer that performs the
upload. The transformer is chosen by `stream.type`.

### `HTTPStreaming/v1alpha1`

Streams a matched resource's content directly to an HTTP endpoint (typically a
`PUT` upload) and rewrites the resource to a `Wget/v1` access pointing at the
uploaded location. The source content is piped straight into the request body, so
it is never buffered in memory or on disk. The digest is computed during the
stream, or — when the source resource already carries one — verified as the bytes
pass through.

Its fields map field-for-field onto the resulting
[`Wget/v1`]({{< relref "docs/reference/input-and-access-types.md" >}}) access:

| Field         | Type                  | Maps to (`Wget/v1`) | Description                                                         |
|---------------|-----------------------|---------------------|---------------------------------------------------------------------|
| `targetURL`   | CEL expression        | `url`               | The upload URL. See Target URL Expressions below.                   |
| `method`      | string                | `verb`              | HTTP method for the upload request. Defaults to PUT.                |
| `header`      | `map[string][]string` | `header`            | HTTP headers to send with the upload request.                       |
| `body`        | bytes                 | `body`              | Optional request body carried on the resulting Wget/v1 access.      |
| `noRedirect`  | bool                  | `noRedirect`        | Disable following HTTP redirects.                                   |
| `mediaType`   | string                | `mediaType`         | Media type recorded on the resource. Defaults to the source's.      |

#### Target URL Expressions

`targetURL` is a [CEL](https://cel.dev/) expression — the same expression language
the transfer graph uses to resolve every other field. It **must be wrapped in
`${…}`**, matching how every other CEL field is written in the transfer graph. It
is evaluated against the source resource, exposed under the `resource` alias, and
resolved by the transfer runtime, so the produced plan is deterministic. CEL string
concatenation (`+`), conditionals (`cond ? a : b`), and comparisons are all
available. Append any static query string inside the expression.

The `resource` alias exposes:

| Expression                        | Value                                                        |
|-----------------------------------|--------------------------------------------------------------|
| `resource.name`                   | Resource name.                                               |
| `resource.version`                | Resource version.                                            |
| `resource.access.url`             | Full source URL.                                             |
| `resource.access.path`            | Path component of the source URL.                            |
| `resource.access.host`            | Host component of the source URL.                            |
| `resource.access.scheme`          | Scheme (`http`/`https`) of the source URL.                   |
| `resource.access.mediaType`       | Media type of the source access.                             |
| `resource.extraIdentity.<key>`    | A value from the resource's extra identity.                  |
| `resource.labels.<name>`          | A resource label value (JSON string values are unquoted).    |

Examples:

```yaml
# Preserve the source path under a new host
targetURL: '${"https://mytarget.example.com" + resource.access.path}'

# Route by name and version
targetURL: '${"https://cdn.example.com/" + resource.name + "/" + resource.version + "/blob"}'

# Use extra identity / labels
targetURL: '${"https://" + resource.labels.region + ".example.com/" + resource.extraIdentity.arch + resource.access.path}'

# Conditional target
targetURL: '${resource.labels.tier == "public" ? "https://cdn.example.com" + resource.access.path : "https://internal.example.com" + resource.access.path}'
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
