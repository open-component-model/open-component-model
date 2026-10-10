---
title: "Input and Access Types"
description: "Reference for input and access types used to add resources to a component version."
weight: 4
toc: true
aliases:
  - /docs/guides/input_and_access/
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

{{< schema-renderer url="/schemas/bindings/go/input/dir/v1/Dir.schema.json" >}}

**Example**

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

{{< schema-renderer url="/schemas/bindings/go/input/file/v1/File.schema.json" >}}

**Example**

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

See the [Working with OCI]({{< relref "docs/guides/pack/add-oci-artifacts.md" >}}) tutorial for a complete walkthrough.

### `Helm/v1`

Embeds a Helm chart from the local filesystem or a remote repository. Exactly one of `path` or `helmRepository` must be
specified.

{{< schema-renderer url="/schemas/bindings/go/input/helm/v1/Helm.schema.json" >}}

**Example**

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

{{< schema-renderer url="/schemas/bindings/go/input/utf8/v1/UTF8.schema.json" >}}

**Example**

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

Alternative type names `wget/v1`, `Wget`, and `wget` are also accepted, as are the additional aliases `HTTP/v1`,
`HTTP`, `http/v1`, and `http`; `Wget/v1` is canonical.

{{< schema-renderer url="/schemas/bindings/go/input/wget/v1/Wget.schema.json" >}}

{{< callout context="caution" >}}
Do not put credentials in `url`, `header`, or `body`. That includes userinfo (`https://user:token@host/...`) and
presigned query parameters. The input specification is resolved at construction time and is not written to the
component descriptor, but it does live in your `component-constructor.yaml`, which is normally checked into version
control. Configure authentication through the
[credential system]({{< relref "credential-consumer-identities.md" >}}#wget) instead, which keeps secrets in
`.ocmconfig` and out of the artifacts you publish.
{{< /callout >}}

**Example**

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

See [Tutorial: Work with HTTP Resources]({{< relref "docs/guides/pack/add-http-resources.md" >}}) for media type
resolution, redirects, download tuning, and credential configuration.

#### Verifying a pinned digest {#wget-pinned-digest}

If the resource carries a `digest` (the standard OCM digest info, set alongside
the resource rather than inside `input`), the wget input verifies the downloaded
content against it and fails the build on a mismatch. Only the canonical
SHA-256 / `genericBlobDigest/v1` form is accepted.

```yaml
resources:
- name: release-archive
  type: blob
  version: 1.0.0
  digest:
    hashAlgorithm: SHA-256
    normalisationAlgorithm: genericBlobDigest/v1
    value: b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9
  input:
    type: Wget/v1
    url: https://downloads.example.com/myapp/1.0.0/myapp-linux-amd64.tar.gz
```

#### Checksum verification (via OCM config) {#checksum-verification-via-ocm-config}

HTTP downloads can additionally be verified against a source-side checksum
advertised in response headers, steered centrally by the
`checksum.http.config.ocm.software/v1alpha1` configuration (the wget spec itself
carries no checksum field). See
[HTTP Checksum Configuration]({{< relref "checksum-http-configuration.md" >}})
for the schema, checksum modes, and the access-side pin-from-source fast path.

### `S3/v2` {#s3v2-input}

Downloads a single object from an S3 or S3-compatible bucket while OCM constructs the component version, and stores it
as a local blob. Use this input type when the content must travel with the component version. Two examples are an
air-gapped delivery, and a bucket that the user of the component version cannot reach. The access type is the
alternative: it leaves the object in the bucket and reads it on every download.

`S3/v2` is the canonical type name. OCM also accepts `s3/v2`, `S3` and `s3`. The fields are the
same as the fields of the
[`S3/v2` access type]({{< relref "input-and-access-types.md" >}}#s3v2-access). You can therefore give the
same object by value or by reference. Input types remain v2-only: even the unversioned aliases require
`bucketName` and `objectKey`; `S3/v1` and `s3/v1` are access types only.

{{< schema-renderer url="/schemas/bindings/go/input/s3/v2/S3.schema.json" >}}

**Example**

```yaml
resources:
  - name: reference-dataset
    type: blob
    version: 1.0.0
    input:
      type: S3/v2
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
      type: S3/v2
      endpoint: https://minio.internal:9000
      usePathStyle: true
      bucketName: acme-artifacts
      objectKey: datasets/reference/1.0.0/reference.parquet
```

{{< callout context="note" >}}
The specification carries no credentials, and no field of it can carry them. Configure authentication in the
[credential system]({{< relref "credential-consumer-identities.md" >}}#s3). It resolves an `S3` consumer
entry from `.ocmconfig`. If no entry matches, the AWS default credential chain applies: environment variables, the
shared AWS config, and IAM instance or task roles. An in-cluster build therefore needs no key material in the OCM
configuration. For unsigned public-object downloads, set `anonymous: true` in
[`S3Credentials/v1`]({{< relref "credential-types.md#s3credentialsv1" >}}), not in the input specification.
This optional boolean defaults to `false`; missing credentials or authentication errors never enable it automatically.
{{< /callout >}}

OCM streams the object to a file under the `tempFolder` of the `filesystem.config.ocm.software/v1alpha1` configuration
type. It does not hold the object in memory, so the size of the object does not change the memory use.

OCM v1 has no S3 input type.

### `Git/v1` {#gitv1-input}

Archives a snapshot of a Git repository while OCM constructs the component version, and stores it as a local blob. The
blob is a gzip-compressed tar (`application/x-tgz`) of the files at the selected commit, without the `.git` directory.
Use this input type when the source code must travel with the component version. The
[`Git/v1` access type]({{< relref "input-and-access-types.md#gitv1-access" >}}) is the alternative: it leaves the code in the repository and reads it on every
download.

`Git/v1` is the canonical type name. OCM also accepts `Git`, and the OCM v1 names `git` and `git/v1`. The fields are
the same as the fields of the access type.

{{< schema-renderer url="/schemas/bindings/go/input/git/v1/Git.schema.json" >}}

If you set neither `ref` nor `commit`, OCM archives the commit that the repository's `HEAD` points to, usually the default
branch. The result then changes when the branch moves. Set `commit` for a reproducible build.

**Example**

```yaml
resources:
  - name: source-snapshot
    type: directoryTree
    version: 1.0.0
    input:
      type: Git/v1
      repository: https://github.com/open-component-model/ocm.git
      ref: refs/tags/v0.39.0
```

{{< callout context="caution" >}}
Do not put credentials in `repository`, such as `https://user:token@host/...`. The URL is in your
`component-constructor.yaml`, which is normally checked into version control. Configure authentication through the
[credential system]({{< relref "credential-consumer-identities.md" >}}#git) instead. The input type and the access type
use the same `Git` consumer identity, so one consumer entry covers both.
{{< /callout >}}

OCM streams the archive to a file under the `tempFolder` of the `filesystem.config.ocm.software/v1alpha1`
configuration type. For the archive format and its digest, see
[Resource Repositories: Git]({{< relref "resource-repositories.md" >}}#git-resource-repository).

The OCM v1 `git` input type has the same fields. A constructor file written for OCM v1 works without changes.

## Access Types

### `OCIImage/v1`

References an OCI artifact (image or image index) in a registry. This is the canonical type name. The legacy aliases
`ociArtifact`, `ociRegistry`, and `ociImage` are also accepted.

{{< schema-renderer url="/schemas/bindings/go/access/oci/v1/OCIImage.schema.json" >}}

**Example**

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
Typically created automatically when using input types or when transferring with a local blob uploader configuration.

When stored in an OCI registry, local blobs with OCI-native media types (e.g. `application/vnd.oci.image.manifest.v1+json`, `application/vnd.oci.image.index.v1+json`) are mapped to native OCI manifests and can be accessed directly by digest using standard OCI tools. The `globalAccess` field provides the native image reference for direct access. See the [Working with OCI]({{< relref "docs/guides/pack/add-oci-artifacts.md" >}}) tutorial for details.

{{< schema-renderer url="/schemas/bindings/go/access/localblob/v1/LocalBlob.schema.json" >}}

**Example**

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

{{< schema-renderer url="/schemas/bindings/go/access/oci/v1/OCIImageLayer.schema.json" >}}

**Example**

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

### `relativeOciReference/v1`

A **read/transfer compatibility type** for component versions migrated from OCM v1. It references an OCI artifact by a
reference that is **relative to the registry root** that hosts the component version. Legacy (unversioned) alias:
`relativeOciReference`.

{{< schema-renderer url="/schemas/bindings/go/access/oci/v1/RelativeOCIReference.schema.json" >}}

Semantics:

- The `reference` is a registry-relative OCI reference `repository[:tag][@digest]` — no scheme, no leading slash, no
  host. A dotted first segment (e.g. `acme.org/value`) is a repository path, not a host.
- Resolution base is the **registry root** of the hosting repository, **not** the OCM `subPath` and never the
  `component-descriptors` path. For `reference: ocm/value:v2.0` in a repository whose registry is `registry.example`
  (even with OCM subPath `ocm-prefix`), the artifact is `registry.example/ocm/value:v2.0`. A prefix already present in
  `reference` is preserved verbatim; only the registry is prepended.
- Digest beats tag; both are preserved when present.
- Resolution reuses the component repository's credentials and transport. Distinct per-namespace credentials for the
  relative artifact are not supported.

This type is **never emitted on write** by OCM v2. It is read like a local resource, and on transfer it is copied
**by value** into the target (becoming a `LocalBlob/v1`), or **re-uploaded as an OCI image** when an
[OCI uploader]({{< relref "transfer-configuration/oci-uploader" >}}) is configured.

**Example**

```yaml
resources:
  - name: value
    type: ociImage
    version: 2.0.0
    relation: local
    access:
      type: relativeOciReference/v1
      reference: ocm/value:v2.0
```

### `Helm/v1`

References a Helm chart in a Helm chart repository or OCI registry. Legacy alias: `helm`.

{{< schema-renderer url="/schemas/bindings/go/access/helm/v1/Helm.schema.json" >}}

**Example**

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

{{< schema-renderer url="/schemas/bindings/go/access/github/v1/GitHub.schema.json" >}}

At least one of `commit` or `ref` must be set. A resource may be authored with only a
`ref`; its `commit` is pinned later during digest processing. A source is never pinned, so
give it a `commit`.

**Example**

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

### `Git/v1` {#gitv1-access}

References a commit in any Git repository. OCM fetches the commit over the Git protocol and returns the files as a
gzip-compressed tar (`application/x-tgz`). It works with every Git server: GitHub, GitLab, Gitea, Azure DevOps,
Bitbucket and self-hosted servers, over HTTPS or SSH.

`Git/v1` is the canonical type name. OCM also accepts `Git`, and the OCM v1 names `git`, `git/v1alpha1` and
`Git/v1alpha1`.

{{< schema-renderer url="/schemas/bindings/go/access/git/v1/Git.schema.json" >}}

At least one of `commit` or `ref` must be set. A resource may have only a `ref`. OCM adds the `commit` during digest
processing. A source never goes through digest processing, so set its `commit` yourself.

**Example**

```yaml
resources:
  - name: my-source
    version: 1.0.0
    type: directoryTree
    relation: external
    access:
      type: Git/v1
      repository: https://gitlab.com/example-group/example-project.git
      ref: refs/heads/main
      commit: a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2
```

The same access works under `sources:`. A source always stays a remote reference. It has no digest, and a transfer
never copies it into the component version.

#### Supported URL forms {#git-url-forms}

| Form          | Example                                           |
|---------------|---------------------------------------------------|
| HTTPS or HTTP | `https://gitlab.com/group/project.git`            |
| SSH URL       | `ssh://git@git.example.com:2222/org/repo.git`     |
| scp-style SSH | `git@github.com:org/repo.git`                     |
| Git protocol  | `git://git.example.com/org/repo.git`              |
| Local         | `file:///srv/git/repo.git` or `/srv/git/repo.git` |

{{< callout context="caution" >}}
A URL needs a scheme, such as `https://github.com/org/repo`. Without one, OCM treats the value as a **local
directory**, so `github.com/org/repo` points to a folder under the current working directory. scp-style SSH addresses
such as `git@github.com:org/repo.git` are the exception and always mean SSH.
{{< /callout >}}

OCM checks the server's host key against your `~/.ssh/known_hosts`. Without a configured SSH key, OCM uses the SSH agent.

#### `Git/v1` or `GitHub/v1`?

Both types reference a commit and return a tar archive of it, but they fetch it in different ways.

| Use                      | When                                                                                                                                               |
|--------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------|
| [`GitHub/v1`](#githubv1) | The repository is on GitHub or GitHub Enterprise and you have HTTPS access to its REST API. The archive is one HTTP download, with no Git history. |
| `Git/v1`                 | The repository is not on GitHub (GitLab, Gitea, Azure DevOps, Bitbucket, a self-hosted server), you need SSH, or the REST API is not reachable.    |

The two archives are not the same bytes, so the same commit has different digests with `GitHub/v1` and `Git/v1`. Do not
switch a published resource from one type to the other.

#### Migrating from OCM v1 {#git-migration-from-ocm-v1}

The OCM v1 `git` access type has the same fields: `repository`, `ref` and `commit`. OCM v2 reads access
specifications with the types `git`, `git/v1alpha1` and `Git/v1alpha1` unchanged. New specifications should use
`Git/v1`.

Two differences need attention:

- **Scheme.** OCM v1 read `github.com/org/repo` as a remote repository. OCM v2 reads it as a local directory, so
  add the scheme. scp-style SSH addresses such as `git@github.com:org/repo.git` are unaffected.
- **Credentials.** The consumer identity type is still `Git`, but the OCM v1 `pathprefix` attribute is gone. Use
  `path`, which supports glob patterns. See
  [Credential Consumer Identities: Git]({{< relref "credential-consumer-identities.md" >}}#git).

### `File/v1alpha1`

References a file by URI ([RFC 8089](https://datatracker.ietf.org/doc/html/rfc8089)). Legacy alias: `file`.

{{< schema-renderer url="/schemas/bindings/go/access/file/v1alpha1/File.schema.json" >}}

**Example**

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
[Tutorial: Work with HTTP Resources]({{< relref "docs/guides/pack/add-http-resources.md#pin-a-digest" >}}).

`Wget/v1` is the canonical type name. OCM also accepts `wget/v1`, `Wget`, `wget`, and the additional aliases
`HTTP/v1`, `HTTP`, `http/v1`, and `http`. The fields are identical to those of the
[`Wget/v1` input type]({{< relref "input-and-access-types.md" >}}#wgetv1-input), so the same request can be expressed
either by value or by reference.

{{< schema-renderer url="/schemas/bindings/go/access/wget/v1/Wget.schema.json" >}}

{{< callout context="caution" >}}
**Never put credentials in `url`, `header`, or `body`.** Unlike an input specification, an access specification is
stored **verbatim in the component descriptor**. Everything written here, including userinfo
(`https://user:token@host/...`) and presigned query parameters in `url`, is persisted with the component version,
travels with it through every transfer, is covered by its signature, and is readable by anyone who can read the
component version. Configure authentication through the
[credential system]({{< relref "credential-consumer-identities.md" >}}#wget) instead. Credentials are resolved at
request time from `.ocmconfig` and never become part of the component version.
{{< /callout >}}

**Example**

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
A plain HTTP endpoint has no standardized write API, so transfer cannot write to the source
location. Without a matching uploader, a `Wget/v1` access stays by reference in the target. A
local blob uploader downloads the content and stores it as a
[`LocalBlob/v1`]({{< relref "input-and-access-types.md" >}}#localblobv1). An HTTP, Artifactory
or Nexus uploader uploads the content to its configured target and publishes a new remote
access there (see [Transfer Configuration]({{< relref "docs/reference/transfer-configuration/_index.md" >}})).
{{< /callout >}}

The `checksum.http.config.ocm.software/v1alpha1` configuration can pin the
access digest from source-advertised response headers via a single HEAD, with
no body download. See
[HTTP Checksum Configuration]({{< relref "checksum-http-configuration.md" >}}).

For guidance on choosing between the input and the access type, and for media type resolution, redirects, download
tuning, and credential configuration, see
[How-To: Add Resources from HTTP URLs]({{< relref "docs/guides/pack/add-http-resources.md" >}}).

### `S3/v2` {#s3v2-access}

References a single object in an S3 or S3-compatible bucket. The content stays in the bucket. OCM reads it when it
downloads the resource, and when it computes the digest of the resource. The type addresses one object, not a whole
repository. It does not make S3 a component version repository.

`S3/v2` and `s3/v2` use `bucketName` and `objectKey`. Explicit `S3/v1` and `s3/v1` use `bucket` and `key`;
both versions support `region`, `version`, `mediaType` and the `endpoint` and `usePathStyle` extensions.
Unversioned `S3` and `s3` accept either legacy `bucket` and `key` or v2 `bucketName` and `objectKey` fields.
Records containing fields from both formats are rejected as ambiguous. See
[Migrating from OCM v1](#s3-migration-from-ocm-v1). The schema below describes the v2 fields.

{{< schema-renderer url="/schemas/bindings/go/access/s3/v2/S3.schema.json" >}}

**Example**

```yaml
resources:
  - name: reference-dataset
    type: blob
    version: 1.0.0
    relation: external
    access:
      type: S3/v2
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
      type: S3/v2
      endpoint: https://minio.internal:9000
      usePathStyle: true
      bucketName: acme-artifacts
      objectKey: datasets/reference/1.0.0/reference.parquet
```

{{< callout context="note" >}}
The specification carries no credentials, and no field of it can carry them. A URL-based access type has places to
hide userinfo or a presigned query string; this type has none, so OCM writes no secret into the component descriptor.
Configure authentication in the
[credential system]({{< relref "credential-consumer-identities.md" >}}#s3). It resolves an `S3` consumer
entry from `.ocmconfig`. If no entry matches, the AWS default credential chain applies: environment variables, the
shared AWS config, and IAM instance or task roles. For unsigned public-object downloads, set `anonymous: true` in
[`S3Credentials/v1`]({{< relref "credential-types.md#s3credentialsv1" >}}), not in the access specification.
This optional boolean defaults to `false`; missing credentials or authentication errors never enable it automatically.
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

When digest pinning changes a v1 access specification, OCM writes it as explicit `S3/v2` with
`bucketName` and `objectKey`. Unversioned v2 aliases retain their type. Unchanged specifications retain their original form.

If you need reproducibility, enable bucket versioning, or set `version`.

{{< callout context="note" >}}
The S3 resource repository does not support upload. OCM never writes an object into a bucket; digest pinning only
updates the access specification.
{{< /callout >}}

#### Migrating from OCM v1 {#s3-migration-from-ocm-v1}

`S3/v2` is the `v2` format of the OCM v1 `s3` access type, plus the fields `endpoint` and `usePathStyle`. OCM v2 reads
an access specification that OCM v1 wrote in the `v2` format without changes.

OCM v2 also reads the OCM v1 `v1` format when the type is explicitly `s3/v1` or `S3/v1`.
Legacy descriptors using the unversioned `s3` that OCM v1 writes by default are also accepted with
`bucket` and `key`; no type change is required. Unversioned records using `bucketName` and `objectKey` retain v2 semantics.
Alternatively, set the type to `S3/v2` and rename the fields as shown below. The `endpoint` and `usePathStyle` extensions are available for both access formats.

| OCM v1 (`s3/v1`) | OCM v1 (`s3/v2`) | OCM v2 (`S3/v2`)     |
|------------------|------------------|----------------------|
| `bucket`         | `bucketName`     | `bucketName`         |
| `key`            | `objectKey`      | `objectKey`          |
| `region`         | `region`         | `region`             |
| `version`        | `version`        | `version`            |
| `mediaType`      | `mediaType`      | `mediaType`          |
| —                | —                | `endpoint` (new)     |
| —                | —                | `usePathStyle` (new) |

```yaml
# Explicit v1 format
access:
  type: s3/v1
  region: eu-central-1
  bucket: acme-artifacts
  key: datasets/reference/1.0.0/reference.parquet
  mediaType: application/vnd.apache.parquet
```

```yaml
# Equivalent explicit v2 format
access:
  type: S3/v2
  region: eu-central-1
  bucketName: acme-artifacts
  objectKey: datasets/reference/1.0.0/reference.parquet
  mediaType: application/vnd.apache.parquet
```

**Behavior.** Both versions support download only. OCM v1 reached AWS S3 only. The fields `endpoint` and
`usePathStyle` are new in OCM v2, and they make S3-compatible stores such as MinIO, Ceph and R2 usable. OCM v1 also
reads `S3/v2`, but it ignores these two fields and reads from AWS S3. Integrity comes from the OCM SHA-256 digest over
the content, in OCM v1 and in OCM v2. It does not come from the S3 `ETag`.

**Credentials.** The consumer identity type is `S3` in both versions, but the attribute that holds the object location
and the credential property names changed. See
[Credential Consumer Identities: Migrating from OCM v1]({{< relref "credential-consumer-identities.md" >}}#s3-identity-migration-from-ocm-v1).
