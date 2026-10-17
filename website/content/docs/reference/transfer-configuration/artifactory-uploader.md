---
title: "JFrog Artifactory Uploader"
description: "Reference for artifactory.uploader.transfer.config.ocm.software/v1alpha1: upload resources into JFrog Artifactory helm, generic, maven and npm repositories."
weight: 6
toc: true
---

Uploads a matched resource into a local repository of a JFrog Artifactory server,
the way the repository's package type expects. For step-by-step guides per
repository type, see
[JFrog Artifactory]({{< relref "docs/guides/transfer/upload-to-jfrog-artifactory.md" >}}).

{{< callout context="note" >}}
The OCM Kubernetes controller ignores Artifactory uploader entries because they
send content to configured URLs from the controller pod.
{{< /callout >}}

## Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/ArtifactoryUploaderConfig.schema.json" >}}

## Fields

| Field        | Type              | Description                                                                                                                                                                                                                                                            |
|--------------|-------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `match`      | CEL expression    | A CEL boolean expression selecting the resources this uploader handles. **Required:** the Artifactory uploader has no default match.                                                                                                                                   |
| `url`        | string (required) | Server base URL **without** the `/artifactory` segment, e.g. `https://myorg.jfrog.io`.                                                                                                                                                                                 |
| `repository` | string (required) | Repository key, e.g. `helm-local`.                                                                                                                                                                                                                                     |
| `path`       | string            | Content location relative to the repository root. Literal or `${…}` CEL expression (see [CEL Expressions]({{< relref "docs/reference/transfer-configuration/cel-expressions.md" >}})). Must be relative, without `.`/`..` segments; helm and npm need a `.tgz` suffix. |

## Repository types

The uploader reads the package type from
`GET <url>/artifactory/api/repositories/<repository>`:

| Type      | Uploaded content                                                                                                                                        | Published access                                                                                                          | `path`                                                  | Guide                                                                                          |
|-----------|---------------------------------------------------------------------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------|---------------------------------------------------------|------------------------------------------------------------------------------------------------|
| `helm`    | The packaged chart found in the content (`.tgz`, a tar holding one, or a Helm chart OCI artifact). Non-chart content is deleted and fails the transfer. | `Helm/v1` with `helmRepository: <url>/artifactory/api/helm/<repository>` and `helmChart: <name>:<version>` from the chart | Optional, must end in `.tgz`                            | [Upload Helm Charts]({{< relref "docs/guides/transfer/upload-to-jfrog-artifactory.md" >}})     |
| `generic` | The content as is; OCI artifacts as an OCI layout tar                                                                                                   | `Wget/v1` on the stored file                                                                                              | Optional                                                | [Upload Generic Files]({{< relref "docs/guides/transfer/upload-to-jfrog-artifactory.md" >}})   |
| `maven`   | As `generic`                                                                                                                                            | `Wget/v1` on the stored file (the timestamped file for `-SNAPSHOT` versions)                                              | Optional; Maven only resolves paths in the Maven layout | [Upload Maven Artifacts]({{< relref "docs/guides/transfer/upload-to-jfrog-artifactory.md" >}}) |
| `npm`     | The npm package tarball as is. Non-package content is deleted and fails the transfer.                                                                   | `Wget/v1` on the stored tarball                                                                                           | Optional, must end in `.tgz`                            | [Upload npm Packages]({{< relref "docs/guides/transfer/upload-to-jfrog-artifactory.md" >}})    |

Only local and federated repositories accept uploads; other package types fail with
`has package type "<type>"; supported: helm, generic, maven, npm`.

## Repository behaviour by type

Beyond the uploaded content and published access in the table above, each package type
has deployment behaviour that affects how consumers resolve the artifact:

### `helm`

- The published chart name and version come from the chart itself, not from the OCM
  resource.
- The repository must **not** enforce chart name and version in file names (*Helm
  Enforce Layout*): the stored file name comes from the OCM resource, so an enforced
  layout rejects it.
- A repeated transfer uploads nothing and logs
  `reused helm chart content already stored in the helm repository`.

### `maven`

- `latest` and `release` in `maven-metadata.xml` are the **highest** version in the
  repository, not the most recently uploaded one. Transferring `0.9.0` after `1.0.0`
  keeps `1.0.0` as `latest` and `release`.
- A `-SNAPSHOT` version is stored under a unique timestamped version, for example
  `…/2.0.0-SNAPSHOT/demo-2.0.0-20260928.183609-1.jar`. The published `Wget/v1` URL
  points at that file, so it keeps returning the transferred bytes after later
  snapshots.
- Maven only resolves artifacts stored under the Maven layout
  `<group path>/<artifactId>/<version>/<artifactId>-<version>.<ext>`, and resolution
  needs a POM. Without a Maven-layout `path` the file lands under the default path:
  it can be downloaded from its `Wget/v1` URL, but Maven does not see it.

### `npm`

- Package name and version come from `package.json`, not from the OCM resource. The
  file name and a custom `path` do not matter to npm, but a custom `path` must end in
  `.tgz`, because Artifactory only indexes `.tgz` files as packages.
- The `latest` dist-tag moves to the **most recently deployed** version, even an older
  one: transferring `1.0.0` after `2.0.0` makes `1.0.0` the `latest`. Restore it with
  `npm dist-tag add <pkg>@<version> latest` after transferring older versions.
- A repeated transfer reuses the stored tarball and logs
  `reused content already stored in the artifactory repository`.

### `generic`

- Any access type is accepted and the content is stored as is.
- OCI images are uploaded as one gzipped OCI layout tar
  (`application/vnd.ocm.software.oci.layout.v1+tar+gzip`).
- A repeated transfer reuses the stored file and logs
  `reused content already stored in the artifactory repository`.

## Default path

Content is deployed to `<url>/artifactory/<repository>/<path>`. The default path is
`<component>/<component version>/<resource>-<resource version>`. For resources with
an extra identity, `-<16-hex-digit hash of the extra identity>` is appended to the
file name, so that every resource gets its own file. `.tgz` is appended for helm and
npm repositories.

## Existing files {#owner-properties}

Every deployed file carries properties naming the resource it was uploaded
for: `ocm.component.name`, `ocm.component.version`, `ocm.resource.name`,
`ocm.resource.version` and, when the resource has one,
`ocm.resource.extraIdentity`. They make the artifact searchable by component and
decide whether a file already stored at the upload path may be replaced:

- no file, or a file with the same content: the file is (re)used;
- a file whose properties name the same resource of the same component
  version: it is replaced (a repeated transfer);
- any other file: the transfer fails and the file is left untouched. Configure
  a `path` that includes whatever distinguishes the resources, such as the
  component version.

## Digest

A `genericBlobDigest/v1` SHA-256 or SHA-512 source digest is verified, and
content the server already stores is not uploaded again. For SHA-256, Artifactory
verifies bytes against an `X-Checksum-Sha256` header on deploy and deploys stored
content by checksum; a SHA-512 digest is verified after the upload, and a
mismatching file is deleted. Content extracted from an OCI artifact
gets the SHA-256 of the uploaded bytes (the OCI layout tar or chart .tgz).

## Credentials

Resolved for the `HelmChartRepository` identity of
`<url>/artifactory/api/helm/<repository>`, falling back to the `Wget` identity
of `<url>/artifactory/<repository>`:

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

`WgetCredentials/v1` additionally supports a bearer `identityToken` and mutual TLS
(`certificate`/`privateKey`); `HelmHTTPCredentials/v1` `certFile`/`keyFile` are
not supported. See
[Credential Consumer Identities]({{< relref "docs/reference/credential-consumer-identities.md" >}}).

## Example

Helm chart upload to Artifactory:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: artifactory.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("Helm")
    url: https://myorg.jfrog.io
    repository: helm-local
```
