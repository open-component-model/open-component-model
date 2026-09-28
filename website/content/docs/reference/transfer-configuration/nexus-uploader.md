---
title: "Sonatype Nexus Uploader"
description: "Reference for nexus.uploader.transfer.config.ocm.software/v1alpha1: upload matched resources into Sonatype Nexus helm, raw, maven2 and npm hosted repositories."
weight: 4
toc: true
---

Uploads a matched resource into a hosted repository of a Sonatype Nexus
Repository 3 server. The behaviour depends on the **format** of the repository,
which the uploader reads from the Nexus repository settings
(`GET <url>/service/rest/v1/repositories/<repository>`):

| Repository type | Source handling                                                             | Published access                                            |
|-----------------|-----------------------------------------------------------------------------|-------------------------------------------------------------|
| `helm`          | The packaged Helm chart located in the resource content (.tgz, tar, or OCI) | `Helm/v1` (`helmRepository: <url>/repository/<repository>`) |
| `raw`           | The resource content as is (OCI artifacts as an OCI layout tar)             | `Wget/v1` (`url: <url>/repository/<repository>/<path>`)     |
| `maven2`        | The resource content as one file of a Maven component                       | `Wget/v1` (`url: <url>/repository/<repository>/<path>`)     |
| `npm`           | The npm package tarball, uploaded through the components API                | `Wget/v1` (`url` of the stored tarball)                     |

The repository **must** be a hosted repository. Proxy and group repositories
cannot receive uploads. The uploading user must be allowed to read the
repository settings.

For step-by-step guidance, including Maven artifacts, see
[Upload Resources to Sonatype Nexus]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus.md" >}}).

## Helm repositories

Nexus stores the chart as `<name>-<version>.tgz` from `Chart.yaml` and
maintains `index.yaml` itself. The uploader looks up the chart by SHA-256 in the
component search. If the repository disables redeploy and already stores the same
content, the rejected upload (`409`) is accepted; different content under the same
name and version fails. `path` is **not supported** for Nexus helm repositories:
Nexus stores charts under a path it derives from the chart itself.

## Raw repositories

The resource content is uploaded to `<url>/repository/<repository>/<path>`.
The default path is `<component>/<component-version>/<resource>-<resource-version>`.
Nexus records no owner of a file, so a file already stored at the path is
**never overwritten**: it is reused when it has the same content, otherwise the
transfer fails with an error asking the user to configure a different path.

## Maven repositories

The resource content is uploaded as one file of a Maven component to
`<url>/repository/<repository>/<path>`. `path` is required and must follow the
Maven repository layout
`<group path>/<artifactId>/<version>/<artifactId>-<version>[-<classifier>].<extension>`;
the coordinates are taken from it. Release versions go through the components
API (`POST /service/rest/v1/components`), which keeps `maven-metadata.xml` up to
date; snapshot versions, which the components API refuses, are uploaded with a
plain `PUT` and are not added to `maven-metadata.xml`. Like raw files, a stored
file is never overwritten.

## npm repositories

The npm package tarball is uploaded through the components API. Nexus reads name
and version from its `package.json`, stores it under
`<name>/-/<name>-<version>.tgz` and keeps `latest` on the highest release
version. The resource is published as `Wget/v1` on the stored tarball; a tarball
the repository already stores is reused. `path` is not supported.

## Digest

A `genericBlobDigest/v1` SHA-256 source digest is verified, and content the
server already stores is not uploaded again. Nexus cannot reject mismatching
bytes on deploy, so the uploader fails after the upload on a mismatch. Content
extracted from an OCI artifact gets the SHA-256 of the uploaded bytes.

## Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/NexusUploaderConfig.schema.json" >}}

## Fields

| Field        | Type              | Description                                                                                                                                                                                                                                                                                                        |
|--------------|-------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `match`      | `UploaderMatch`   | Selects resources this uploader applies to. `match.accessType` is required.                                                                                                                                                                                                                                        |
| `url`        | string (required) | Server base URL **without** the `/repository` segment, e.g. `https://nexus.example.com`.                                                                                                                                                                                                                           |
| `repository` | string (required) | Repository name, e.g. `helm-hosted`.                                                                                                                                                                                                                                                                               |
| `path`       | string            | Content location relative to the root; required in Maven repository layout for `maven2` repositories. Literal or `${…}` CEL expression (see [CEL Expressions]({{< relref "docs/reference/transfer-configuration/cel-expressions.md" >}})). Must be relative, without `.`/`..` segments. Not for helm or npm repos. |

## Sources

**Helm** repositories accept any access type; the chart archive is located in the
content, exactly as described for [Artifactory helm repositories]({{< relref "docs/reference/transfer-configuration/artifactory-uploader.md" >}}#helm-repositories).

**Raw** and **Maven** repositories accept any access type and upload the
resource content as is. OCI artifacts are materialized as an OCI layout tar.

## Credentials

Resolved for the `HelmChartRepository` identity of
`<url>/repository/<repository>`, falling back to its `Wget` identity:

```yaml
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: HelmChartRepository
          hostname: nexus.example.com
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

Helm chart upload to Nexus:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: nexus.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: Helm/v1
    url: https://nexus.example.com
    repository: helm-hosted
```

Raw repository — upload any resource and publish a `Wget/v1` access:

```yaml
  - type: nexus.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: localBlob
    url: https://nexus.example.com
    repository: raw-hosted
```
