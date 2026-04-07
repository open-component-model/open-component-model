---
title: "Input and Access Types"
description: "Reference for input and access types used to add resources to a component version."
weight: 4
toc: true
---

## Overview

Resources in a component version are added using either an **input type** or an **access type**.

- **Input type** — embeds content *by value*. The content is stored alongside the component descriptor in the target repository.
- **Access type** — stores an access specification pointing to the content. In the constructor, this typically references an external location (e.g. an OCI registry) rather than embedding the content.

A resource must have exactly one of `input` or `access`. See the [Component Constructor]({{< relref "component-constructor.md" >}}) reference for the full YAML schema.

## Input Types

### `dir/v1`

Embeds a directory as a tar archive.

| Field | Type | Required | Description                                                                                                                                                                                                        |
| ----- | ---- | -------- |--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `path` | string | yes | Path to the directory (relative to the constructor file).                                                                                                                                                          |
| `mediaType` | string | no | MediaType of the resource (defaults to application/x-tar). The Dir input always creates a tar. However, it does not add a +tar suffix as this might cause conflicts with MediaType's such as application/x-tar. |
| `compress` | boolean | no | Compress the tar archive (gzip). If set to true, adds a +gzip suffix to the MediaType.                                                                                                                             |
| `reproducible` | boolean | no | Normalize file attributes (timestamps, permissions) for reproducible digests. Recommended when signing.                                                                                                            |
| `preserveDir` | boolean | no | Include the directory itself in the archive.                                                                                                                                                                       |
| `followSymlinks` | boolean | no | Include the content of symbolic links in the archive. Not yet implemented; accepted for compatibility with previous OCM versions.                                                                                  |
| `excludeFiles` | array of string | no | Glob patterns for files to exclude.                                                                                                                                                                                |
| `includeFiles` | array of string | no | Glob patterns for files to include.                                                                                                                                                                                |

```yaml
resources:
- name: deploy-manifests
  type: blob
  input:
    type: dir/v1
    path: ./deploy
    compress: true
    reproducible: true
```

### `file/v1`

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
    type: file/v1
    path: ./config.yaml
    mediaType: application/yaml
```

### `helm/v1`

Embeds a Helm chart from the local filesystem or a remote repository. Exactly one of `path` or `helmRepository` must be specified.

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| `path` | string | no | Path to a local chart directory or `.tgz` archive. |
| `helmRepository` | string | no | Remote URL (HTTP/HTTPS `.tgz` or OCI reference). |
| `repository` | string | no | OCI reference specifying the upload location of the chart. Must include a version tag matching the chart version (e.g. `charts/myapp:1.0.0`). |

```yaml
# Local chart
resources:
- name: my-chart
  type: helmChart
  input:
    type: helm/v1
    path: ./charts/myapp
    repository: charts/myapp:1.0.0
---
# Remote chart (HTTP)
resources:
- name: ingress-chart
  type: helmChart
  input:
    type: helm/v1
    helmRepository: https://github.com/kubernetes/ingress-nginx/releases/download/helm-chart-4.14.0/ingress-nginx-4.14.0.tgz
---
# Remote chart (OCI)
resources:
- name: podinfo-chart
  type: helmChart
  input:
    type: helm/v1
    helmRepository: oci://ghcr.io/stefanprodan/charts/podinfo:6.9.1
    repository: charts/podinfo:6.9.1
```

{{< callout type="info" >}}
The deprecated aliases `helm` and `Helm` are still accepted but `helm/v1` is the preferred form.
{{< /callout >}}

### `utf8/v1`

Embeds inline text or structured data. Exactly one of `text`, `json`, `formattedJson`, or `yaml` must be specified.

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| `text` | string | no | Plain text content. |
| `json` | any | no | JSON value (stored compact). |
| `formattedJson` | any | no | JSON value (stored formatted). |
| `yaml` | any | no | YAML value (converted to JSON for storage). |
| `compress` | boolean | no | Compress the content (gzip). |

```yaml
resources:
- name: config-data
  type: blob
  input:
    type: utf8/v1
    json:
      replicas: 3
      env: production
```

## Access Types

### `OCIImage/v1`

References an OCI artifact (image or image index) in a registry. This is the canonical type name. The legacy aliases `ociArtifact`, `ociRegistry`, and `ociImage` are also accepted.

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| `imageReference` | string | yes | Full OCI image reference including registry, repository, and tag or digest. |

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

### `localBlob/v1`

References content stored alongside the component descriptor in the same repository. Typically created automatically when using input types.

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| `localReference` | string | yes | Repository-local blob identifier (usually a digest). |
| `mediaType` | string | yes | Media type of the blob. |
| `referenceName` | string | no | Optional static name for the blob in a local repository context. |
| `globalAccess` | object | no | Optional global access fallback. |

```yaml
resources:
- name: data
  type: blob
  relation: local
  access:
    type: localBlob/v1
    localReference: sha256:57563cb4a3e5c06a22c95aaa445...
    mediaType: application/octet-stream
```

### `OCIImageLayer/v1`

References a single blob (layer) in an OCI repository by digest. Legacy alias: `ociBlob`.

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| `ref` | string | yes | OCI repository reference. |
| `mediaType` | string | no | Media type of the layer. |
| `digest` | string | yes | Digest of the blob. |
| `size` | integer | yes | Size of the blob in bytes. |

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

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| `helmRepository` | string | yes | URL of the Helm chart repository. |
| `helmChart` | string | yes | Chart name and optional version separated by `:` (e.g. `mariadb:12.2.7`). |
| `version` | string | no | Chart version. Can also be specified as part of `helmChart`. |

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

### `File/v1alpha1`

References a file by URI ([RFC 8089](https://datatracker.ietf.org/doc/html/rfc8089)). Legacy alias: `file`.

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| `uri` | string | yes | File locator conforming to RFC 8089. |
| `mediaType` | string | no | Media type of the file. Inferred from the file extension if not set. |
| `digest` | string | no | Expected content digest for integrity verification (e.g. `sha256:7173b809...`). OCI digest format. |

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
