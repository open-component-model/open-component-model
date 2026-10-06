---
title: "Sonatype Nexus Uploader"
description: "Reference for nexus.uploader.transfer.config.ocm.software/v1alpha1: upload matched resources into Sonatype Nexus helm, raw, maven2 and npm hosted repositories."
weight: 7
toc: true
---

Uploads a matched resource into a hosted repository of a Sonatype Nexus
Repository 3 server, the way the repository's format expects. For step-by-step
guides per repository type, see
[Sonatype Nexus]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/_index.md" >}}).

{{< callout context="note" >}}
The OCM Kubernetes controller ignores Nexus uploader entries because they send
content to configured URLs from the controller pod.
{{< /callout >}}

## Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/NexusUploaderConfig.schema.json" >}}

## Fields

| Field        | Type              | Description                                                                                                                                                                                                                                                                                                        |
| ------------ | ----------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `match`      | CEL expression    | A CEL boolean expression selecting the resources this uploader handles. **Required:** the Nexus uploader has no default match.                                                                                                                                                                                     |
| `url`        | string (required) | Server base URL **without** the `/repository` segment, e.g. `https://nexus.example.com`.                                                                                                                                                                                                                           |
| `repository` | string (required) | Repository name, e.g. `helm-hosted`.                                                                                                                                                                                                                                                                               |
| `path`       | string            | Content location relative to the root; required in Maven repository layout for `maven2` repositories. Literal or `${…}` CEL expression (see [CEL Expressions]({{< relref "docs/reference/transfer-configuration/cel-expressions.md" >}})). Must be relative, without `.`/`..` segments. Not for helm or npm repos. |

## Repository types

The uploader reads the format from
`GET <url>/service/rest/v1/repositories/<repository>`:

| Type     | Uploaded content                                                                                                                                                        | Published access                                               | `path`                                                                                                            | Guide                                                                                                         |
| -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| `helm`   | The packaged chart found in the content (`.tgz`, a tar holding one, or a Helm chart OCI artifact); stored as `<name>-<version>.tgz`                                     | `Helm/v1` with `helmRepository: <url>/repository/<repository>` | Not supported                                                                                                     | [Upload Helm Charts]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/helm-charts.md" >}})         |
| `raw`    | The content as is; OCI artifacts as an OCI layout tar                                                                                                                   | `Wget/v1` on the [pinned download URL](#pinned-download-url)   | Optional                                                                                                          | [Upload Raw Files]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/raw-files.md" >}})             |
| `maven2` | The content as one file of a Maven component. Releases go through the components API; `-SNAPSHOT` versions use a plain `PUT` and are not added to `maven-metadata.xml`. | `Wget/v1` on the [pinned download URL](#pinned-download-url)   | Required, in Maven layout `<group path>/<artifactId>/<version>/<artifactId>-<version>[-<classifier>].<extension>` | [Upload Maven Artifacts]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/maven-artifacts.md" >}}) |
| `npm`    | The tarball via the components API; stored as `<name>/-/<name>-<version>.tgz`                                                                                           | `Wget/v1` on the [pinned download URL](#pinned-download-url)   | Not supported                                                                                                     | [Upload npm Packages]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/npm-packages.md" >}})       |

Only hosted repositories accept uploads; other formats fail with
`has format "<format>"; supported: helm, raw, maven2, npm`.

## Default path

Content is uploaded to `<url>/repository/<repository>/<path>`. The default path is
`<component>/<component version>/<resource>-<resource version>`. For resources with
an extra identity, `-<16-hex-digit hash of the extra identity>` is appended to the
file name, so that every resource gets its own file. The default path applies to
`raw` repositories only.

## Existing files

Nexus records no owner of a file, so in `raw` and `maven2` repositories a stored file
is never overwritten: same content is reused, different content fails the transfer.
In `helm` and `npm` repositories a stored package with the same content is reused,
and different content under the same name and version fails when the repository
disallows redeploys.

## Digest

A `genericBlobDigest/v1` SHA-256 or SHA-512 source digest is verified, and
content the server already stores is not uploaded again. Nexus cannot reject mismatching
bytes on deploy, so the uploader fails after the upload on a mismatch. Content
extracted from an OCI artifact gets the SHA-256 of the uploaded bytes.

## Pinned download URL

Nexus sends no SHA-256 checksum header with a file, only a SHA-1 `ETag`. A `Wget/v1`
access is therefore published on the asset search download endpoint, which selects the
file by its SHA-256 (or SHA-512) and the file's name or coordinates:

```text
<url>/service/rest/v1/search/assets/download?name=%2F<path>&repository=<repository>&sha256=<hex>
```

Nexus redirects the URL to the stored file only while the file has that content, and
answers `404` otherwise. With
[checksum mode]({{< relref "docs/reference/checksum-http-configuration.md#access-digest" >}})
`Require` or `Prefer`, OCM pins the resource digest from the URL with a single
credentialed `HEAD`, and the download verifies the bytes against it.
`maven2` repositories select the file by its Maven coordinates, `npm` repositories by the
checksum alone. `-SNAPSHOT` versions and content with an unknown digest are published
on `<url>/repository/<repository>/<path>`.

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

## Example

Helm chart upload to Nexus:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: nexus.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("Helm")
    url: https://nexus.example.com
    repository: helm-hosted
```
