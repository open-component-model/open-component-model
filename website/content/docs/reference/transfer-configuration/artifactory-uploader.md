---
title: "JFrog Artifactory Uploader"
description: "Reference for artifactory.uploader.transfer.config.ocm.software/v1alpha1: upload matched resources into JFrog Artifactory helm, generic, maven and npm repositories."
weight: 3
toc: true
---

Uploads a matched resource into a local repository of a JFrog Artifactory server.
The behaviour depends on the **package type** of the repository, which the uploader
reads from the Artifactory repository configuration
(`GET <url>/artifactory/api/repositories/<repository>`):

| Repository type    | Source handling                                                             | Published access                                                                                     |
|--------------------|-----------------------------------------------------------------------------|------------------------------------------------------------------------------------------------------|
| `helm`             | The packaged Helm chart located in the resource content (.tgz, tar, or OCI) | `Helm/v1` (`helmRepository: <url>/artifactory/api/helm/<repository>`, `helmChart: <name>:<version>`) |
| `generic`, `maven` | The resource content as is (OCI artifacts as an OCI layout tar)             | `Wget/v1` (`url` of the stored file)                                                                 |
| `npm`              | The npm package tarball, as is                                              | `Wget/v1` (`url` of the stored file)                                                                 |

The repository **must** be a local or federated repository. Remote or virtual
repositories cannot receive uploads. The uploading user must be allowed to read
the repository configuration.

For step-by-step guidance per repository type, including Maven POMs and npm
packages, see
[Upload Resources to JFrog Artifactory]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory.md" >}}).

## Helm repositories

Artifactory reads the chart name and version from the deployed file's
properties; content without them is deleted and fails the transfer. Artifactory
indexes the deployed chart itself. The repository must not enable
*Enforce Chart Name and Version* (Helm Enforce Layout), because the file name
does not come from the chart.

## Owner properties

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

## Path

Content is deployed to `<url>/artifactory/<repository>/<path>`. The default path
is `<component>/<component-version>/<resource>-<resource-version>`, plus `.tgz`
for helm repositories. A custom `path` must be relative, without `.`/`..`
segments, and end in `.tgz` for helm repositories.

## Digest

A `genericBlobDigest/v1` SHA-256 source digest is verified, and content the
server already stores is not uploaded again. Artifactory verifies bytes against an
`X-Checksum-Sha256` header on deploy. Content extracted from an OCI artifact
gets the SHA-256 of the uploaded bytes (the OCI layout tar or chart .tgz).

## Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/ArtifactoryUploaderConfig.schema.json" >}}

## Fields

| Field        | Type              | Description                                                                                                                                                                                                                                                            |
|--------------|-------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `match`      | `UploaderMatch`   | Selects resources this uploader applies to. `match.accessType` is required.                                                                                                                                                                                            |
| `url`        | string (required) | Server base URL **without** the `/artifactory` segment, e.g. `https://myorg.jfrog.io`.                                                                                                                                                                                 |
| `repository` | string (required) | Repository key, e.g. `helm-local`.                                                                                                                                                                                                                                     |
| `path`       | string            | Content location relative to the repository root. Literal or `${…}` CEL expression (see [CEL Expressions]({{< relref "docs/reference/transfer-configuration/cel-expressions.md" >}})). Must be relative, without `.`/`..` segments; helm and npm need a `.tgz` suffix. |

## Sources

**Helm** repositories accept any access type; the chart archive is located in the
content: a packaged chart `.tgz`, a tar containing one (Helm downloader output),
or a Helm chart OCI artifact (its chart layer is uploaded). Other content fails
the transfer.

**Generic** and **Maven** repositories accept any access type and store the
resource content as is. OCI artifacts are materialized as an OCI layout tar.
The file is not packaged as a Maven artifact: it is downloadable by its URL,
but Maven only resolves it if its content and `path` already follow the Maven
layout. When Artifactory stores a file under another path than requested,
such as a Maven `-SNAPSHOT` file under its timestamped version, the published
`url` points at the stored file.

**npm** repositories accept any access type holding an npm package tarball.
Artifactory reads its `package.json` and serves the version through its npm API;
content it does not recognize as a package is deleted again and fails the
transfer. Artifactory moves the `latest` dist-tag to the most recently deployed
version, also when that is an older version.

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

## Examples

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

Artifactory with the default path written out as `path` (for resources without
extra identity), as a starting point for your own layout. Keep
`component.version` in it unless each chart version comes from a single
component version:

```yaml
  - type: artifactory.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Helm/v1
    url: https://myorg.jfrog.io
    repository: helm-local
    path: '${component.name + "/" + component.version + "/" + resource.name + "-" + resource.version + ".tgz"}'
```

Generic repository — upload any resource and publish a `Wget/v1` access:

```yaml
  - type: artifactory.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: localBlob
    url: https://myorg.jfrog.io
    repository: generic-local
```
