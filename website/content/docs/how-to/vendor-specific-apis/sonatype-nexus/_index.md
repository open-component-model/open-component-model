---
title: "Upload Resources to Sonatype Nexus"
description: "Upload resources into Sonatype Nexus Repository 3 hosted helm, maven2, npm and raw repositories during transfer, with credentials and overwrite rules."
weight: 2
toc: true
sidebar:
  collapsed: true
---

The
[`nexus.uploader.transfer.config.ocm.software/v1alpha1`]({{< relref "docs/reference/transfer-configuration/nexus-uploader.md" >}})
uploader uploads the resources it matches into one Nexus hosted repository during transfer.
Each repository format has its own page below with a complete configuration.

## Repository types

| Format   | Guide                                                                                                         | Published access |
|----------|---------------------------------------------------------------------------------------------------------------|------------------|
| `helm`   | [Upload Helm Charts]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/helm-charts.md" >}})         | `Helm/v1`        |
| `maven2` | [Upload Maven Artifacts]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/maven-artifacts.md" >}}) | `Wget/v1`        |
| `npm`    | [Upload npm Packages]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/npm-packages.md" >}})       | `Wget/v1`        |
| `raw`    | [Upload Raw Files]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/raw-files.md" >}})             | `Wget/v1`        |

At upload time the uploader reads the format from the server with
`GET <url>/service/rest/v1/repositories/<repository>`.

### Unsupported repositories

| Repository                  | Error                                                  | Alternative                                 |
|-----------------------------|--------------------------------------------------------|---------------------------------------------|
| `pypi` and other formats    | `has format "pypi"; supported: helm, raw, maven2, npm` | Upload with the format's own client         |
| Proxy or group repositories | `uploads need a hosted repository`                     | Upload into the hosted repository behind it |

## Prerequisites

- [OCM CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) installed
- A component version in a CTF or OCI repository
- A Nexus **hosted** repository
- A user that may upload into the repository **and read its settings**

## Configure credentials

The uploader resolves credentials for the `HelmChartRepository` identity of
`<url>/repository/<repository>`, falling back to its `Wget` identity. A `Wget`
consumer without a path covers every repository on the server:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: Wget
          hostname: nexus.example.com
          scheme: https
        credentials:
          - type: WgetCredentials/v1
            username: <USERNAME>
            password: <PASSWORD_OR_USER_TOKEN>
```

{{< callout context="caution" >}}
`hostname` is the bare host name. A value such as `https://nexus.example.com`
matches no request, so the upload is sent without credentials and fails with `401`.
{{< /callout >}}

## Existing files at the upload path

Nexus records no owner of a file, so the uploader never replaces stored content:

- **`raw` and `maven2`:** a stored file is never overwritten, even when the repository
  allows redeploys. Same content is reused; different content fails the transfer.
- **`helm` and `npm`:** Nexus stores them under a path derived from the package. A stored
  package with the same content is reused. Different content under the same name and
  version fails when the repository disallows redeploys.

## Troubleshooting

### Symptom: `failed detecting the type of … repository …: GET … returned status 401`

**Cause:** No credentials were found for the server, so the uploader asked anonymously.

**Fix:** Configure credentials as shown above, with the bare host name in `hostname`.

### Symptom: `failed detecting the type of … repository …: GET … returned status 403`

**Cause:** The user may deploy but not read the repository settings.

**Fix:** Grant read access to the repository configuration, or use a user that has it.

### Symptom: `nexus repository … already stores a different file at …; the uploader never overwrites files in … repositories`

**Cause:** The raw or maven2 repository already stores a file with different content at the
upload path, for example from another component version with the same resource
version.

**Fix:** Configure a `path` that is unique for the resource, or remove the stored file.

## Related documentation

- [How-to: Upload Resources to JFrog Artifactory]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/_index.md" >}})
- [Reference: Sonatype Nexus Uploader]({{< relref "docs/reference/transfer-configuration/nexus-uploader.md" >}})
- [Reference: Transfer Configuration]({{< relref "docs/reference/transfer-configuration/_index.md" >}})
- [Tutorial: Configure Custom Uploads During Transfer]({{< relref "docs/tutorials/configure-custom-uploads.md" >}})
- [Concept: Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}})
- [Credential Consumer Identities]({{< relref "docs/reference/credential-consumer-identities.md" >}})
