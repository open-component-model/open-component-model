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

### `Wget/v1` {#wgetv1-input}

Downloads content from an HTTP or HTTPS URL while the component version is constructed and embeds it as a local blob.
Use it when the upstream artifact is a plain HTTP download (a release archive, a checksum file, a signed binary) and you
want the bytes captured in the component version rather than fetched again at consumption time.

Alternative type names `wget/v1`, `Wget`, and `wget` are also accepted; `Wget/v1` is canonical.

| Field        | Type                  | Required | Description                                                                                                                               |
|--------------|-----------------------|----------|-------------------------------------------------------------------------------------------------------------------------------------------|
| `url`        | string                | yes      | HTTP or HTTPS endpoint to download from. Other URL schemes are rejected.                                                                  |
| `mediaType`  | string                | no       | Media type of the downloaded content. If omitted, the response `Content-Type` header is used, falling back to `application/octet-stream`. |
| `header`     | `map[string][]string` | no       | Additional HTTP headers to send with the request.                                                                                         |
| `verb`       | string                | no       | HTTP method to use. Defaults to `GET`.                                                                                                    |
| `body`       | string (base64)       | no       | Request body. Encoded as base64 in YAML because the underlying field is a byte slice.                                                     |
| `noRedirect` | boolean               | no       | Do not follow HTTP redirects. Defaults to `false`.                                                                                        |

{{< callout context="caution" >}}
Do not put credentials in `url`, `header`, or `body`. That includes userinfo (`https://user:token@host/...`) and
presigned query parameters. The input specification is resolved at construction time and is not written to the
component descriptor, but it does live in your `component-constructor.yaml`, which is normally checked into version
control. Configure authentication through the
[credential system]({{< relref "credential-consumer-identities.md" >}}#wget) instead, which keeps secrets in
`.ocmconfig` and out of the artifacts you publish.
{{< /callout >}}

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

See [Tutorial: Work with HTTP Resources]({{< relref "docs/tutorials/wget-http-resources.md" >}}) for media type
resolution, redirects, download tuning, and credential configuration.

### `S3Bucket/v1` {#s3bucketv1-input}

Downloads a single object from an S3 or S3-compatible bucket while OCM constructs the component version, and stores it
as a local blob. Use this input type when the content must travel with the component version. Two examples are an
air-gapped delivery, and a bucket that the user of the component version cannot reach. The access type is the
alternative: it leaves the object in the bucket and reads it on every download.

`S3Bucket/v1` is the canonical type name. OCM also accepts `s3Bucket/v1`, `S3Bucket` and `s3Bucket`. The fields are the
same as the fields of the
[`S3Bucket/v1` access type]({{< relref "input-and-access-types.md" >}}#s3bucketv1-access). You can therefore give the
same object by value or by reference.

| Field          | Type    | Required | Description                                                                                                                                                        |
|----------------|---------|----------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `bucketName`   | string  | yes      | Name of the bucket that holds the object.                                                                                                                          |
| `objectKey`    | string  | yes      | Key (path) of the object in the bucket.                                                                                                                            |
| `region`       | string  | no       | Region of the bucket. If you omit it, the AWS SDK reads `AWS_REGION` or the shared AWS config, and falls back to `us-east-1`. Most S3-compatible stores ignore it. |
| `mediaType`    | string  | no       | Media type of the object. If you omit it, OCM uses the `Content-Type` of the object, and falls back to `application/octet-stream`.                                 |
| `version`      | string  | no       | S3 object version (`versionId`) to read. If you omit it, OCM reads the latest version.                                                                             |
| `endpoint`     | string  | no       | Base endpoint of an S3-compatible store such as MinIO, Ceph or R2, for example `https://minio.internal:9000`. If you omit it, OCM uses AWS S3.                     |
| `usePathStyle` | boolean | no       | Put the bucket in the path (`<endpoint>/<bucket>/<key>`) instead of in the host. Most self-hosted S3-compatible stores need this. Default: `false`.                |

```yaml
resources:
  - name: reference-dataset
    type: blob
    version: 1.0.0
    input:
      type: S3Bucket/v1
      region: eu-central-1
      bucketName: acme-artifacts
      objectKey: datasets/reference/1.0.0/reference.parquet
      mediaType: application/vnd.apache.parquet
```

For an S3-compatible store, set the endpoint and use path-style addressing:

```yaml
resources:
  - name: reference-dataset
    type: blob
    version: 1.0.0
    input:
      type: S3Bucket/v1
      endpoint: https://minio.internal:9000
      usePathStyle: true
      bucketName: acme-artifacts
      objectKey: datasets/reference/1.0.0/reference.parquet
```

{{< callout context="note" >}}
The specification carries no credentials, and no field of it can carry them. Configure authentication in the
[credential system]({{< relref "credential-consumer-identities.md" >}}#s3bucket). It resolves an `S3Bucket` consumer
entry from `.ocmconfig`. If no entry matches, the AWS default credential chain applies: environment variables, the
shared AWS config, and IAM instance or task roles. An in-cluster build therefore needs no key material in the OCM
configuration.
{{< /callout >}}

OCM streams the object to a file under the `tempFolder` of the `filesystem.config.ocm.software/v1alpha1` configuration
type. It does not hold the object in memory, so the size of the object does not change the memory use.

OCM v2 does not accept the OCM v1 `s3` access method under any of its old names. See
[Migrating from OCM v1](#s3-migration-from-ocm-v1).

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

{{< callout context="note" >}}
For Helm charts stored in OCI registries, use the [`OCIImage/v1`]({{< relref "input-and-access-types.md" >}}#ociimagev1)
access type instead. The [Helm resource repository]({{< relref "resource-repositories.md" >}}#helm-resource-repository)
only supports HTTP/HTTPS-based chart repositories.
{{< /callout >}}

### `GitHub/v1`

References a commit of a GitHub repository, downloaded as a source archive via the GitHub
REST API. Also usable unversioned as `GitHub`. Legacy aliases: `github`, `github/v1`,
`gitHub`, `gitHub/v1`.

| Field         | Type   | Required | Description                                                                                                                      |
|---------------|--------|----------|----------------------------------------------------------------------------------------------------------------------------------|
| `repoUrl`     | string | yes      | Repository URL (scheme optional, `https` assumed), e.g. `github.com/open-component-model/ocm`.                                   |
| `apiHostname` | string | no       | Overrides the GitHub REST API hostname for GitHub Enterprise.                                                                    |
| `commit`      | string | no*      | 40-character hex commit SHA. When set it is authoritative.                                                                       |
| `ref`         | string | no*      | Git reference (e.g. `refs/heads/main`), resolved to a commit at download time and pinned onto the resource by digest processing. |

\* At least one of `commit` or `ref` must be set. A resource may be authored with only a
`ref`; its `commit` is pinned later during digest processing. A source is never pinned, so
give it a `commit`.

```yaml
resources:
  - name: my-source
    version: 1.0.0
    type: directoryTree
    relation: external
    access:
      type: GitHub/v1
      repoUrl: https://github.com/open-component-model/ocm
      commit: a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2
```

The same access works under `sources:`, where it stays a remote reference: sources carry no
digest and no copy mode embeds them.

{{< callout context="note" >}}
Any `repoUrl` host other than `github.com` is treated as GitHub Enterprise, with the REST API
on that same host. Set `apiHostname` only when the API lives on a different host.

`apiHostname` and the optional `commit` extend the OCM spec's `gitHub` access type: the spec
lists `commit` as required and has no `apiHostname` attribute.
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

{{< callout context="caution" >}}
This access type is **alpha** (`v1alpha1`). Its schema may change in future releases.
{{< /callout >}}

### `Wget/v1` {#wgetv1-access}

References content served over HTTP or HTTPS. The bytes stay on the remote server and are fetched when the resource
is downloaded, when its digest is computed, and when the component version is transferred.

Because the content is not under your control, the expected digest can be pinned on the resource itself, using the
`digest` field alongside `access` rather than inside it. It is then verified on every fetch. See
[Tutorial: Work with HTTP Resources]({{< relref "docs/tutorials/wget-http-resources.md#pin-a-digest" >}}).

Alternative type names `wget/v1`, `Wget`, and `wget` are also accepted; `Wget/v1` is canonical. The fields are identical
to those of the [`Wget/v1` input type]({{< relref "input-and-access-types.md" >}}#wgetv1-input), so the same request can
be expressed either by value or by reference.

| Field        | Type                  | Required | Description                                                                                                                               |
|--------------|-----------------------|----------|-------------------------------------------------------------------------------------------------------------------------------------------|
| `url`        | string                | yes      | HTTP or HTTPS endpoint to download from. Other URL schemes are rejected.                                                                  |
| `mediaType`  | string                | no       | Media type of the referenced content. If omitted, the response `Content-Type` header is used, falling back to `application/octet-stream`. |
| `header`     | `map[string][]string` | no       | Additional HTTP headers to send with the request.                                                                                         |
| `verb`       | string                | no       | HTTP method to use. Defaults to `GET`.                                                                                                    |
| `body`       | string (base64)       | no       | Request body. Encoded as base64 in YAML because the underlying field is a byte slice.                                                     |
| `noRedirect` | boolean               | no       | Do not follow HTTP redirects. Defaults to `false`.                                                                                        |

{{< callout context="caution" >}}
**Never put credentials in `url`, `header`, or `body`.** Unlike an input specification, an access specification is
stored **verbatim in the component descriptor**. Everything written here, including userinfo
(`https://user:token@host/...`) and presigned query parameters in `url`, is persisted with the component version,
travels with it through every transfer, is covered by its signature, and is readable by anyone who can read the
component version. Configure authentication through the
[credential system]({{< relref "credential-consumer-identities.md" >}}#wget) instead. Credentials are resolved at
request time from `.ocmconfig` and never become part of the component version.
{{< /callout >}}

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

{{< callout context="note" >}}
Upload is not supported for this access type: a plain HTTP endpoint has no standardized write API. A `Wget/v1` access therefore has no by-reference form in a target repository. It is copied only when resource copying is requested using `--copy-resources`, and then always by value.  The content is downloaded and stored as a [`LocalBlob/v1`]({{< relref "input-and-access-types.md" >}}#localblobv1).
{{< /callout >}}

For guidance on choosing between the input and the access type, and for media type resolution, redirects, download
tuning, and credential configuration, see
[How-To: Add Resources from HTTP URLs]({{< relref "docs/how-to/add-resources-from-http-urls.md" >}}).

### `S3Bucket/v1` {#s3bucketv1-access}

References a single object in an S3 or S3-compatible bucket. The content stays in the bucket. OCM reads it when it
downloads the resource, and when it computes the digest of the resource. The type addresses one object, not a whole
repository. It does not make S3 a component version repository.

`S3Bucket/v1` is the canonical type name. OCM also accepts `s3Bucket/v1`, `S3Bucket` and `s3Bucket`. Matching is exact:
an all-lowercase `s3bucket` does not resolve, and the OCM v1 `s3` access type does not resolve. See
[Migrating from OCM v1](#s3-migration-from-ocm-v1).

| Field          | Type    | Required | Description                                                                                                                                                        |
|----------------|---------|----------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `bucketName`   | string  | yes      | Name of the bucket that holds the object.                                                                                                                          |
| `objectKey`    | string  | yes      | Key (path) of the object in the bucket.                                                                                                                            |
| `region`       | string  | no       | Region of the bucket. If you omit it, the AWS SDK reads `AWS_REGION` or the shared AWS config, and falls back to `us-east-1`. Most S3-compatible stores ignore it. |
| `mediaType`    | string  | no       | Media type of the object. If you omit it, OCM uses the `Content-Type` of the object, and falls back to `application/octet-stream`.                                 |
| `version`      | string  | no       | S3 object version (`versionId`) to read. If you omit it, OCM reads the latest version.                                                                             |
| `endpoint`     | string  | no       | Base endpoint of an S3-compatible store such as MinIO, Ceph or R2, for example `https://minio.internal:9000`. If you omit it, OCM uses AWS S3.                     |
| `usePathStyle` | boolean | no       | Put the bucket in the path (`<endpoint>/<bucket>/<key>`) instead of in the host. Most self-hosted S3-compatible stores need this. Default: `false`.                |

```yaml
resources:
  - name: reference-dataset
    type: blob
    version: 1.0.0
    relation: external
    access:
      type: S3Bucket/v1
      region: eu-central-1
      bucketName: acme-artifacts
      objectKey: datasets/reference/1.0.0/reference.parquet
      mediaType: application/vnd.apache.parquet
```

For an S3-compatible store, set the endpoint and use path-style addressing:

```yaml
resources:
  - name: reference-dataset
    type: blob
    version: 1.0.0
    relation: external
    access:
      type: S3Bucket/v1
      endpoint: https://minio.internal:9000
      usePathStyle: true
      bucketName: acme-artifacts
      objectKey: datasets/reference/1.0.0/reference.parquet
```

{{< callout context="note" >}}
The specification carries no credentials, and no field of it can carry them. A URL-based access type has places to
hide userinfo or a presigned query string; this type has none, so OCM writes no secret into the component descriptor.
Configure authentication in the
[credential system]({{< relref "credential-consumer-identities.md" >}}#s3bucket). It resolves an `S3Bucket` consumer
entry from `.ocmconfig`. If no entry matches, the AWS default credential chain applies: environment variables, the
shared AWS config, and IAM instance or task roles.
{{< /callout >}}

#### Object versions and integrity

Integrity comes from the OCM SHA-256 digest over the content, computed with the `genericBlobDigest/v1` normalisation.
OCM does not use the S3 `ETag`, because the `ETag` is not a whole-object hash for a multipart upload. If the resource
already has a digest, OCM compares the computed digest with it. A difference fails the operation.

Digest processing also pins the access to the object version that it read, so a later read gets the same object. A pin
is only possible if the object has a version:

- On a **versioned bucket**, OCM writes the reported `versionId` of the object into `version`. If the specification
  already sets `version`, OCM sends it with the request, and the response must return the same value.
- On an **unversioned bucket**, which is the AWS default, S3 reports the placeholder `null`. The placeholder does not
  change after an overwrite, so it pins nothing, and OCM never writes it into the specification. The resource digest
  still detects a replaced object, so verification fails. OCM does not accept the wrong content.

If you need reproducibility, enable bucket versioning, or set `version`.

{{< callout context="note" >}}
The S3 resource repository does not support upload. OCM never writes an object into a bucket, and never creates an
`S3Bucket/v1` access.
{{< /callout >}}

#### Migrating from OCM v1 {#s3-migration-from-ocm-v1}

OCM v1 had its own S3 access method. It is **not** compatible with `S3Bucket/v1`. OCM matches access type names as
exact strings, so no OCM v1 spelling resolves in OCM v2: `s3`, `s3/v1`, `s3/v2`, `S3`, `S3/v1` and `S3/v2`. You must
rewrite the access specifications of a component descriptor that OCM v1 created. There is no compatibility layer, and
OCM does not convert them at read time.

**Type name and fields.** The OCM v1 `v2` format already used `bucketName` and `objectKey`. For that format, only the
type name changes. The `v1` format used two other field names as well:

| OCM v1 (`s3/v1`) | OCM v1 (`s3/v2`) | OCM v2 (`S3Bucket/v1`) |
|------------------|------------------|------------------------|
| `bucket`         | `bucketName`     | `bucketName`           |
| `key`            | `objectKey`      | `objectKey`            |
| `region`         | `region`         | `region`               |
| `version`        | `version`        | `version`              |
| `mediaType`      | `mediaType`      | `mediaType`            |
| —                | —                | `endpoint` (new)       |
| —                | —                | `usePathStyle` (new)   |

```yaml
# OCM v1
access:
  type: s3/v1
  region: eu-central-1
  bucket: acme-artifacts
  key: datasets/reference/1.0.0/reference.parquet
  mediaType: application/vnd.apache.parquet
```

```yaml
# OCM v2
access:
  type: S3Bucket/v1
  region: eu-central-1
  bucketName: acme-artifacts
  objectKey: datasets/reference/1.0.0/reference.parquet
  mediaType: application/vnd.apache.parquet
```

**Behavior.** Both versions support download only. OCM v1 reached AWS S3 only. The fields `endpoint` and
`usePathStyle` are new in OCM v2, and they make S3-compatible stores such as MinIO, Ceph and R2 usable. Integrity comes
from the OCM SHA-256 digest over the content, in OCM v1 and in OCM v2. It does not come from the S3 `ETag`.

**Credentials.** The consumer identity also changed. The identity type, the attribute that holds the object location,
and the credential property names are all different. See
[Credential Consumer Identities: Migrating from OCM v1]({{< relref "credential-consumer-identities.md" >}}#s3bucket-migration-from-ocm-v1).
