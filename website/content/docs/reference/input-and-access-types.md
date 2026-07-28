---
title: "Input and Access Types"
description: "Reference for input and access types used to add resources to a component version."
weight: 4
toc: true
---

## Overview

Resources in a component version are added using either an **input type** or an **access type**.

- **Input type** — embeds content *by value*. The content is stored alongside the component descriptor in the target
  repository.
- **Access type** — stores an access specification pointing to the content. In the constructor, this typically
  references an external location (e.g. an OCI registry) rather than embedding the content.

A resource must have exactly one of `input` or `access`. See the
[Component Constructor]({{< relref "component-constructor.md" >}})
reference for the full YAML schema.

## Input Types

### `Dir/v1`

Embeds a directory as a tar archive.

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| `path` | string | yes | Path to the directory (relative to the constructor file). |
| `mediaType` | string | no | MediaType of the resource (defaults to application/x-tar). The Dir input always creates a tar. However, it does not add a +tar suffix as this might cause conflicts with MediaType's such as application/x-tar. |
| `compress` | boolean | no | Compress the tar archive (gzip). If set to true, adds a +gzip suffix to the MediaType. |
| `reproducible` | boolean | no | Normalize file attributes (timestamps, permissions) for reproducible digests. Recommended when signing. |
| `preserveDir` | boolean | no | Include the directory itself in the archive. |
| `followSymlinks` | boolean | no | Include the content of symbolic links in the archive. Not yet implemented; accepted for compatibility with previous OCM versions. |
| `excludeFiles` | array of string | no | Glob patterns for files to exclude. |
| `includeFiles` | array of string | no | Glob patterns for files to include. |

```yaml
resources:
- name: deploy-manifests
  type: blob
  input:
    type: Dir/v1
    path: ./deploy
    compress: true
    reproducible: true
```

### `File/v1`

Embeds a single file.

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| `path` | string | yes | Path to the file (relative to the constructor file). |
| `mediaType` | string | no | Media type of the file. |
| `compress` | boolean | no | Compress the content (gzip). |

```yaml
resources:
- name: config
  type: blob
  input:
    type: File/v1
    path: ./config.yaml
    mediaType: application/yaml
```

#### Embedding OCI Image Layouts

The `file/v1` input type can embed OCI image layout tar archives. When the media type is set to `application/vnd.ocm.software.oci.layout.v1+tar`, OCM recognizes the blob as a native OCI artifact and stores it as a proper OCI manifest during transfer to an OCI registry, making it accessible with standard OCI tooling.

```yaml
resources:
- name: my-oci-artifact
  type: ociArtifact
  input:
    type: file/v1
    path: ./oci-artifact.tar
    mediaType: application/vnd.ocm.software.oci.layout.v1+tar
```

See the [Working with OCI]({{< relref "docs/tutorials/working-with-oci" >}}) tutorial for a complete walkthrough.

### `Helm/v1`

Embeds a Helm chart from the local filesystem or a remote repository. Exactly one of `path` or `helmRepository` must be
specified.

| Field            | Type   | Required | Description                                                                                                                                   |
|------------------|--------|----------|-----------------------------------------------------------------------------------------------------------------------------------------------|
| `path`           | string | no       | Path to a local chart directory or `.tgz` archive.                                                                                            |
| `helmRepository` | string | no       | Remote URL (HTTP/HTTPS `.tgz` or OCI reference).                                                                                              |
| `repository`     | string | no       | OCI reference specifying the upload location of the chart. Must include a version tag matching the chart version (e.g. `charts/myapp:1.0.0`). |

```yaml
# Local chart
resources:
- name: my-chart
  type: helmChart
  input:
    type: Helm/v1
    path: ./charts/myapp
    repository: charts/myapp:1.0.0
---
# Remote chart (HTTP)
resources:
- name: ingress-chart
  type: helmChart
  input:
    type: Helm/v1
    helmRepository: https://github.com/kubernetes/ingress-nginx/releases/download/helm-chart-4.14.0/ingress-nginx-4.14.0.tgz
---
# Remote chart (OCI)
resources:
- name: podinfo-chart
  type: helmChart
  input:
    type: Helm/v1
    helmRepository: oci://ghcr.io/stefanprodan/charts/podinfo:6.9.1
    repository: charts/podinfo:6.9.1
```

### `UTF8/v1`

Embeds inline text or structured data. Exactly one of `text`, `json`, `formattedJson`, or `yaml` must be specified.

| Field           | Type    | Required | Description                                 |
|-----------------|---------|----------|---------------------------------------------|
| `text`          | string  | no       | Plain text content.                         |
| `json`          | any     | no       | JSON value (stored compact).                |
| `formattedJson` | any     | no       | JSON value (stored formatted).              |
| `yaml`          | any     | no       | YAML value (converted to JSON for storage). |
| `compress`      | boolean | no       | Compress the content (gzip).                |

```yaml
resources:
- name: config-data
  type: blob
  input:
    type: UTF8/v1
    json:
      replicas: 3
      env: production
```

### `Wget/v1` {#wget-input}

Downloads content from an HTTP or HTTPS URL while the component version is constructed and embeds it as a local blob.
Use it when the upstream artifact is a plain HTTP download (a release archive, a checksum file, a signed binary) and you
want the bytes captured in the component version rather than fetched again at consumption time.

The legacy type names `wget/v1`, `Wget`, and `wget` are also accepted; `Wget/v1` is canonical.

| Field        | Type                | Required | Description                                                                                                                              |
|--------------|---------------------|----------|--------------------------------------------------------------------------------------------------------------------------------------------|
| `url`        | string              | yes      | HTTP or HTTPS endpoint to download from. Other URL schemes are rejected.                                                                 |
| `mediaType`  | string              | no       | Media type of the downloaded content. If omitted, the response `Content-Type` header is used, falling back to `application/octet-stream`. |
| `header`     | map[string][]string | no       | Additional HTTP headers to send with the request.                                                                                        |
| `verb`       | string              | no       | HTTP method to use. Defaults to `GET`.                                                                                                   |
| `body`       | string (base64)     | no       | Request body. Encoded as base64 in YAML because the underlying field is a byte slice.                                                    |
| `noRedirect` | boolean             | no       | Do not follow HTTP redirects. Defaults to `false`.                                                                                       |

```yaml
resources:
- name: release-archive
  type: blob
  version: 1.0.0
  input:
    type: Wget/v1
    url: https://downloads.example.com/myapp/1.0.0/myapp-linux-amd64.tar.gz
    mediaType: application/x-tar+gzip
```

With custom headers and a non-default verb:

```yaml
resources:
- name: report
  type: blob
  version: 1.0.0
  input:
    type: Wget/v1
    url: https://api.example.com/reports
    verb: POST
    mediaType: application/json
    header:
      Accept:
        - application/json
      X-Request-Source:
        - ocm
    # base64 of {"format":"json"}
    body: eyJmb3JtYXQiOiJqc29uIn0=
```

See [Wget behavior](#wget-behavior) for redirects, size limits, and HTTP client settings, and
[Wget authentication](#wget-authentication) for credential configuration.

## Access Types

### `OCIImage/v1`

References an OCI artifact (image or image index) in a registry. This is the canonical type name. The legacy aliases
`ociArtifact`, `ociRegistry`, and `ociImage` are also accepted.

| Field            | Type   | Required | Description                                                                 |
|------------------|--------|----------|-----------------------------------------------------------------------------|
| `imageReference` | string | yes      | Full OCI image reference including registry, repository, and tag or digest. |

```yaml
resources:
  - name: app-image
    type: ociImage
    version: 1.0.0
    relation: external
    access:
      type: OCIImage/v1
      imageReference: ghcr.io/acme/myapp:1.0.0
```

### `LocalBlob/v1`

References content stored alongside the component descriptor in the same repository. Legacy alias: `localBlob`.
Typically created automatically when using input types or when transferring with `--copy-resources`.

When stored in an OCI registry, local blobs with OCI-native media types (e.g. `application/vnd.oci.image.manifest.v1+json`, `application/vnd.oci.image.index.v1+json`) are mapped to native OCI manifests and can be accessed directly by digest using standard OCI tools. The `globalAccess` field provides the native image reference for direct access. See the [Working with OCI]({{< relref "docs/tutorials/working-with-oci" >}}) tutorial for details.

| Field            | Type   | Required | Description                                                      |
|------------------|--------|----------|------------------------------------------------------------------|
| `localReference` | string | yes      | Repository-local blob identifier (usually a digest).             |
| `mediaType`      | string | yes      | Media type of the blob.                                          |
| `referenceName`  | string | no       | Optional static name for the blob in a local repository context. |
| `globalAccess`   | object | no       | Optional global access fallback.                                 |

```yaml
resources:
  - name: data
    type: blob
    relation: local
    access:
      type: LocalBlob/v1
      localReference: sha256:57563cb4a3e5c06a22c95aaa445...
      mediaType: application/octet-stream
```

### `OCIImageLayer/v1`

References a single blob (layer) in an OCI repository by digest. Legacy alias: `ociBlob`.

| Field       | Type    | Required | Description                |
|-------------|---------|----------|----------------------------|
| `ref`       | string  | yes      | OCI repository reference.  |
| `mediaType` | string  | no       | Media type of the layer.   |
| `digest`    | string  | yes      | Digest of the blob.        |
| `size`      | integer | yes      | Size of the blob in bytes. |

```yaml
resources:
  - name: layer-data
    type: blob
    version: 1.0.0
    relation: external
    access:
      type: OCIImageLayer/v1
      ref: ghcr.io/acme/myapp
      digest: sha256:abc123...
      size: 1048576
      mediaType: application/octet-stream
```

### `Helm/v1`

References a Helm chart in a Helm chart repository or OCI registry. Legacy alias: `helm`.

| Field            | Type   | Required | Description                                                               |
|------------------|--------|----------|---------------------------------------------------------------------------|
| `helmRepository` | string | yes      | URL of the Helm chart repository.                                         |
| `helmChart`      | string | yes      | Chart name and optional version separated by `:` (e.g. `mariadb:12.2.7`). |
| `version`        | string | no       | Chart version. Can also be specified as part of `helmChart`.              |

```yaml
resources:
  - name: mariadb-chart
    type: helmChart
    version: 12.2.7
    relation: external
    access:
      type: Helm/v1
      helmChart: mariadb:12.2.7
      helmRepository: https://charts.bitnami.com/bitnami
```

{{< callout type="note" >}}
For Helm charts stored in OCI registries, use the [`OCIImage/v1`]({{< relref "input-and-access-types.md" >}}#ociimagev1)
access type instead. The [Helm resource repository]({{< relref "resource-repositories.md" >}}#helm-resource-repository)
only supports HTTP/HTTPS-based chart repositories.
{{< /callout >}}

### `File/v1alpha1`

References a file by URI ([RFC 8089](https://datatracker.ietf.org/doc/html/rfc8089)). Legacy alias: `file`.

| Field       | Type   | Required | Description                                                                                        |
|-------------|--------|----------|----------------------------------------------------------------------------------------------------|
| `uri`       | string | yes      | File locator conforming to RFC 8089.                                                               |
| `mediaType` | string | no       | Media type of the file. Inferred from the file extension if not set.                               |
| `digest`    | string | no       | Expected content digest for integrity verification (e.g. `sha256:7173b809...`). OCI digest format. |

```yaml
resources:
  - name: readme
    type: blob
    relation: external
    access:
      type: File/v1alpha1
      uri: file:///path/to/readme.md
      mediaType: text/markdown
```

{{< callout type="warning" >}}
This access type is **alpha** (`v1alpha1`). Its schema may change in future releases.
{{< /callout >}}

### `Wget/v1` {#wget-access}

References content served over HTTP or HTTPS. The bytes stay on the remote server — they are fetched when the resource
is downloaded, when its digest is computed, and when the component version is transferred.

The legacy type names `wget/v1`, `Wget`, and `wget` are also accepted; `Wget/v1` is canonical. The fields are identical
to those of the [`Wget/v1` input type](#wget-input), so the same request can be expressed either by value or by reference.

| Field        | Type                | Required | Description                                                                                                                              |
|--------------|---------------------|----------|--------------------------------------------------------------------------------------------------------------------------------------------|
| `url`        | string              | yes      | HTTP or HTTPS endpoint to download from. Other URL schemes are rejected.                                                                 |
| `mediaType`  | string              | no       | Media type of the referenced content. If omitted, the response `Content-Type` header is used, falling back to `application/octet-stream`. |
| `header`     | map[string][]string | no       | Additional HTTP headers to send with the request.                                                                                        |
| `verb`       | string              | no       | HTTP method to use. Defaults to `GET`.                                                                                                   |
| `body`       | string (base64)     | no       | Request body. Encoded as base64 in YAML because the underlying field is a byte slice.                                                    |
| `noRedirect` | boolean             | no       | Do not follow HTTP redirects. Defaults to `false`.                                                                                       |

```yaml
resources:
  - name: release-archive
    type: blob
    version: 1.0.0
    relation: external
    access:
      type: Wget/v1
      url: https://downloads.example.com/myapp/1.0.0/myapp-linux-amd64.tar.gz
      mediaType: application/x-tar+gzip
```

{{< callout type="note" >}}
Upload is not supported for this access type — a plain HTTP endpoint has no standardized write API. Consequently, wget
resources are **always transferred by value**: transferring a component version downloads the content and stores it as a
[`LocalBlob/v1`](#localblobv1) in the target repository, so the target no longer depends on the origin server.
{{< /callout >}}

#### Choosing between the input and the access type

| Use the **input type** when                                 | Use the **access type** when                                    |
|-------------------------------------------------------------|-----------------------------------------------------------------|
| The content must be reproducible and available offline      | The URL is the authoritative location and should stay so        |
| The upstream URL may disappear or change content            | The artifact is large and should not be duplicated              |
| You are building an air-gapped or self-contained repository | Consumers are expected to fetch directly from the origin server |

The distinction only holds for as long as the component version stays where it was built. Because wget resources are
always transferred by value, transferring a component version turns its `Wget/v1` access specifications into local blobs
in the target repository.

## Wget Behavior

The following applies to both the `Wget/v1` input type and the `Wget/v1` access type — they share one transport,
credential, and size-limiting implementation.

### Media Type Resolution

The media type of the resulting blob is resolved in this order:

1. The `mediaType` field of the specification, if set.
2. The `Content-Type` header of the HTTP response.
3. `application/octet-stream`.

{{< callout type="note" >}}
Unlike OCM v1, the media type is **not** derived from the URL's file extension. Set `mediaType` explicitly when the
server does not send a useful `Content-Type`.
{{< /callout >}}

### Redirects

Redirects are followed by default. With `noRedirect: true` the request stops at the first redirect response, and because
a 3xx status is not a success status the operation fails with an error reporting that status. Use it to assert that a URL
serves content directly rather than to fetch the redirect target.

### Response Status and Size Limit

Only 2xx responses are accepted; any other status fails the operation. Response bodies are limited to **100 MiB**. The
limit is a safeguard against unbounded downloads and is not configurable from the CLI — it can only be changed by callers
of the Go bindings.

Error messages report the request URL with userinfo, query parameters, and fragment stripped, so credentials embedded in
presigned URLs are not leaked into logs.

### HTTP Client Configuration

Timeouts, retries, connection settings, and per-host overrides come from the `http.config.ocm.software/v1alpha1`
configuration type and apply to every wget request:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    timeout: 2m
    retry:
      maxRetries: 3
    hosts:
      downloads.example.com:
        timeout: 5m
```

See [HTTP Client Configuration]({{< relref "http-client-configuration.md" >}}) for the full schema, defaults, and
per-host merge semantics.

## Wget Authentication

Credentials are resolved from the resource URL. OCM builds a consumer identity of type `Wget` with the `hostname`,
`scheme`, `port`, and `path` taken from the URL, then matches it against the consumers configured in `.ocmconfig`. The
input type and the access type derive the identity the same way, so one entry covers both.

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: Wget
          hostname: downloads.example.com
          scheme: https
        credentials:
          - type: WgetCredentials/v1
            username: my-user
            password: my-password
```

Three authentication mechanisms are supported:

| Mechanism       | Fields                                                         | Notes                                                              |
|-----------------|----------------------------------------------------------------|----------------------------------------------------------------------|
| HTTP Basic Auth | `username`, `password`                                         | Sets the `Authorization` header.                                   |
| Bearer token    | `identityToken`                                                | Sets the `Authorization` header. Takes precedence over Basic Auth. |
| Mutual TLS      | `certificate`, `privateKey`, optionally `certificateAuthority` | Transport-layer; combines with either header-based mechanism.      |

Basic Auth and a bearer token both write the `Authorization` header and are therefore mutually exclusive — when both are
configured the bearer token wins and a warning is logged. A client certificate is only effective over HTTPS; supplying
one for an `http://` URL logs a warning and has no effect.

See [Credential Consumer Identities: Wget]({{< relref "credential-consumer-identities.md" >}}#wget) for matching rules
and [Credential Types: WgetCredentials/v1]({{< relref "credential-types.md" >}}#wgetcredentialsv1) for the full field
reference.

## Migrating wget from OCM v1

The wget access and input types carry over from OCM v1 with the same purpose and the same fields, but a few names
changed:

| OCM v1                                    | OCM v2                                     | Notes                                                                                                             |
|-------------------------------------------|--------------------------------------------|-----------------------------------------------------------------------------------------------------------------------|
| `type: wget`                              | `type: Wget/v1`                            | `wget`, `Wget`, and `wget/v1` remain accepted as deprecated aliases.                                              |
| `noredirect`                              | `noRedirect`                               | Field names are matched case-insensitively, so the old spelling still parses; `noRedirect` is the canonical form. |
| Consumer identity type `wget`             | Consumer identity type `Wget`              | **Breaking.** Identity types are matched by exact string — the lowercase spelling no longer matches.              |
| `pathprefix` in the consumer identity     | `path` in the consumer identity            | **Breaking.** Matching changed from longest-prefix to glob (`*` matches a single path segment).                   |
| Media type deduced from the URL extension | Media type from `mediaType`/`Content-Type` | Set `mediaType` explicitly where OCM v1 relied on the file extension.                                            |

The credential property names (`username`, `password`, `identityToken`, `certificate`, `privateKey`,
`certificateAuthority`) are unchanged, so an existing `Credentials/v1` properties map keeps working once the identity
type is corrected to `Wget`.
