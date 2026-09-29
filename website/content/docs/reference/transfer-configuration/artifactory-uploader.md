---
title: "JFrog Artifactory Uploader"
description: "Reference for artifactory.uploader.transfer.config.ocm.software/v1alpha1: upload resources into JFrog Artifactory helm, generic, maven and npm repositories."
weight: 3
toc: true
---

Uploads a matched resource into a local repository of a JFrog Artifactory server,
the way the repository's package type expects. For step-by-step guides per
repository type, see
[JFrog Artifactory]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/_index.md" >}}).

## Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/ArtifactoryUploaderConfig.schema.json" >}}

## Fields

| Field        | Type              | Description                                                                                                                                                                                                                                                            |
|--------------|-------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `match`      | `UploaderMatch`   | Selects resources this uploader applies to. `match.accessType` is required.                                                                                                                                                                                            |
| `url`        | string (required) | Server base URL **without** the `/artifactory` segment, e.g. `https://myorg.jfrog.io`.                                                                                                                                                                                 |
| `repository` | string (required) | Repository key, e.g. `helm-local`.                                                                                                                                                                                                                                     |
| `path`       | string            | Content location relative to the repository root. Literal or `${…}` CEL expression (see [CEL Expressions]({{< relref "docs/reference/transfer-configuration/cel-expressions.md" >}})). Must be relative, without `.`/`..` segments; helm and npm need a `.tgz` suffix. |

## Repository types

The uploader reads the package type from
`GET <url>/artifactory/api/repositories/<repository>`:

| Type      | Uploaded content                                                                                                                                        | Published access                                                                                                          | `path`                                                  | Guide                                                                                                            |
|-----------|---------------------------------------------------------------------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------|---------------------------------------------------------|------------------------------------------------------------------------------------------------------------------|
| `helm`    | The packaged chart found in the content (`.tgz`, a tar holding one, or a Helm chart OCI artifact). Non-chart content is deleted and fails the transfer. | `Helm/v1` with `helmRepository: <url>/artifactory/api/helm/<repository>` and `helmChart: <name>:<version>` from the chart | Optional, must end in `.tgz`                            | [Upload Helm Charts]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/helm-charts.md" >}})         |
| `generic` | The content as is; OCI artifacts as an OCI layout tar                                                                                                   | `Wget/v1` on the stored file                                                                                              | Optional                                                | [Upload Generic Files]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/generic-files.md" >}})     |
| `maven`   | As `generic`                                                                                                                                            | `Wget/v1` on the stored file (the timestamped file for `-SNAPSHOT` versions)                                              | Optional; Maven only resolves paths in the Maven layout | [Upload Maven Artifacts]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/maven-artifacts.md" >}}) |
| `npm`     | The npm package tarball as is. Non-package content is deleted and fails the transfer.                                                                   | `Wget/v1` on the stored tarball                                                                                           | Optional, must end in `.tgz`                            | [Upload npm Packages]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/npm-packages.md" >}})       |

Only local and federated repositories accept uploads; other package types fail with
`has package type "<type>"; supported: helm, generic, maven, npm`.

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

A `genericBlobDigest/v1` SHA-256 source digest is verified, and content the
server already stores is not uploaded again. Artifactory verifies bytes against an
`X-Checksum-Sha256` header on deploy. Content extracted from an OCI artifact
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
    match:
      accessType: Helm/v1
    url: https://myorg.jfrog.io
    repository: helm-local
```
