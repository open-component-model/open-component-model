---
title: "Sonatype Nexus Uploader"
description: "Reference for nexus.uploader.transfer.config.ocm.software/v1alpha1: upload matched resources into Sonatype Nexus helm, raw, maven2 and npm hosted repositories."
weight: 7
toc: true
---

Uploads a matched resource into a hosted repository of a Sonatype Nexus
Repository 3 server, the way the repository's format expects. For step-by-step
guides per repository type, see
[Sonatype Nexus]({{< relref "docs/guides/transfer/upload-to-sonatype-nexus.md" >}}).

{{< callout context="note" >}}
The OCM Kubernetes controller ignores Nexus uploader entries because they send
content to configured URLs from the controller pod.
{{< /callout >}}

## Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/NexusUploaderConfig.schema.json" >}}

## Fields

| Field        | Type              | Description                                                                                                                                                                                                                                                                                                        |
|--------------|-------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `match`      | CEL expression    | A CEL boolean expression selecting the resources this uploader handles. **Required:** the Nexus uploader has no default match.                                                                                                                                                                                     |
| `url`        | string (required) | Server base URL **without** the `/repository` segment, e.g. `https://nexus.example.com`.                                                                                                                                                                                                                           |
| `repository` | string (required) | Repository name, e.g. `helm-hosted`.                                                                                                                                                                                                                                                                               |
| `path`       | string            | Content location relative to the root; required in Maven repository layout for `maven2` repositories. Literal or `${…}` CEL expression (see [CEL Expressions]({{< relref "docs/reference/transfer-configuration/cel-expressions.md" >}})). Must be relative, without `.`/`..` segments. Not for helm or npm repos. |

## Repository types

The uploader reads the format from
`GET <url>/service/rest/v1/repositories/<repository>`:

| Type     | Uploaded content                                                                                                                                                        | Published access                                               | `path`                                                                                                            | Guide                                                                                       |
|----------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------|
| `helm`   | The packaged chart found in the content (`.tgz`, a tar holding one, or a Helm chart OCI artifact); stored as `<name>-<version>.tgz`                                     | `Helm/v1` with `helmRepository: <url>/repository/<repository>` | Not supported                                                                                                     | [Upload Helm Charts]({{< relref "docs/guides/transfer/upload-to-sonatype-nexus.md" >}})     |
| `raw`    | The content as is; OCI artifacts as an OCI layout tar                                                                                                                   | `Wget/v1` on `<url>/repository/<repository>/<path>`            | Optional                                                                                                          | [Upload Raw Files]({{< relref "docs/guides/transfer/upload-to-sonatype-nexus.md" >}})       |
| `maven2` | The content as one file of a Maven component. Releases go through the components API; `-SNAPSHOT` versions use a plain `PUT` and are not added to `maven-metadata.xml`. | `Wget/v1` on `<url>/repository/<repository>/<path>`            | Required, in Maven layout `<group path>/<artifactId>/<version>/<artifactId>-<version>[-<classifier>].<extension>` | [Upload Maven Artifacts]({{< relref "docs/guides/transfer/upload-to-sonatype-nexus.md" >}}) |
| `npm`    | The tarball via the components API; stored as `<name>/-/<name>-<version>.tgz`                                                                                           | `Wget/v1` on the stored tarball                                | Not supported                                                                                                     | [Upload npm Packages]({{< relref "docs/guides/transfer/upload-to-sonatype-nexus.md" >}})    |

Only hosted repositories accept uploads; other formats fail with
`has format "<format>"; supported: helm, raw, maven2, npm`.

## Repository behaviour by format

Beyond the uploaded content and published access in the table above, each format has
deployment behaviour that affects how consumers resolve the artifact. Nexus records no
owner of a file, so in every format a stored file with the same content is reused and
the uploader never silently replaces different content (see [Existing files](#existing-files)).

### `helm`

- Nexus stores the chart as `<name>-<version>.tgz` taken from its `Chart.yaml` and adds
  it to the repository's `index.yaml`; the published name and version come from the
  chart, not the OCM resource.
- `path` is not supported — Nexus decides where charts are stored.
- A repeated transfer reuses the stored chart. Different content under a chart name and
  version already stored fails when the repository disallows redeploys.

### `maven2`

- **Release** versions are uploaded through the components API
  (`POST /service/rest/v1/components`), which updates `maven-metadata.xml`; its `latest`
  and `release` are the **highest** version, not the most recently uploaded one
  (transferring `4.0.0` after `5.0.0` keeps `5.0.0`).
- **Snapshot** (`-SNAPSHOT`) versions use a plain `PUT` into a snapshot repository such
  as `maven-snapshots`, because the components API refuses them. Maven resolves them by
  exact version, but they are **not** added to `maven-metadata.xml`. Routing a
  `-SNAPSHOT` version to a release repository fails with a version-policy mismatch, so
  route snapshots with a rule declared before the release rules (first match wins).
- A `path` is required and must be in the Maven layout; a POM must declare the
  coordinates of its `path`, or the transfer fails before anything is uploaded.
- Stored files are **never overwritten**.

### `npm`

- The tarball is uploaded through the components API (`POST /service/rest/v1/components`
  with the tarball as `npm.asset`), because Nexus does not accept plain uploads of
  package tarballs.
- Nexus reads name and version from `package.json` and stores the tarball under
  `<name>/-/<name>-<version>.tgz` (`@scope/<name>/-/<name>-<version>.tgz` for scoped
  packages); the scope and name are unchanged by the uploader.
- The `latest` dist-tag is the **highest release** version, not the most recently
  uploaded one, and a prerelease never becomes `latest`.
- A repeated transfer reuses the stored tarball, found by its digest. A different
  version already stored with other content fails with `409` when redeploy is disabled.

### `raw`

- Any access type is accepted and the content is stored as is.
- OCI images are uploaded as one gzipped OCI layout tar
  (`application/vnd.ocm.software.oci.layout.v1+tar+gzip`).
- A stored file is **never overwritten**: a file with different content at the upload
  path fails the transfer with
  `the uploader never overwrites files in raw repositories, configure a different path`.

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
