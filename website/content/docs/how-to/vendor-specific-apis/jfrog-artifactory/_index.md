---
title: "Upload Resources to JFrog Artifactory"
description: "Upload resources into JFrog Artifactory helm, maven, npm and generic repositories during transfer, with credentials and overwrite rules."
weight: 1
toc: true
sidebar:
  collapsed: true
---

The
[`artifactory.uploader.transfer.config.ocm.software/v1alpha1`]({{< relref "docs/reference/transfer-configuration/artifactory-uploader.md" >}})
uploader uploads the resources it matches into one Artifactory repository during transfer.
Each repository type has its own page below with a complete configuration.

## Repository types

| Package type | Guide                                                                                                            | Published access |
|--------------|------------------------------------------------------------------------------------------------------------------|------------------|
| `helm`       | [Upload Helm Charts]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/helm-charts.md" >}})         | `Helm/v1`        |
| `maven`      | [Upload Maven Artifacts]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/maven-artifacts.md" >}}) | `Wget/v1`        |
| `npm`        | [Upload npm Packages]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/npm-packages.md" >}})       | `Wget/v1`        |
| `generic`    | [Upload Generic Files]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/generic-files.md" >}})     | `Wget/v1`        |

At upload time the uploader reads the package type from the server with
`GET <url>/artifactory/api/repositories/<repository>`.

### Unsupported repositories

| Repository                   | Error                                                             | Alternative                                                |
|------------------------------|-------------------------------------------------------------------|------------------------------------------------------------|
| `docker`                     | `has package type "docker"; supported: helm, generic, maven, npm` | Transfer images to the OCI registry host of the repository |
| Remote or virtual repository | `uploads need a local repository`                                 | Upload into the local repository behind it                 |

## Prerequisites

- [OCM CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) installed
- A component version in a CTF or OCI repository
- An Artifactory **local** (or federated) repository
- A user that may deploy into the repository **and read its configuration**

## Configure credentials

The uploader resolves credentials for the `HelmChartRepository` identity of
`<url>/artifactory/api/helm/<repository>`, falling back to the `Wget` identity of
`<url>/artifactory/<repository>`. A `Wget` consumer without a path covers every
repository on the server:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: Wget
          hostname: myorg.jfrog.io
          scheme: https
        credentials:
          - type: WgetCredentials/v1
            identityToken: <ARTIFACTORY_IDENTITY_TOKEN>
```

{{< callout context="caution" >}}
`hostname` is the bare host name. A value such as `https://myorg.jfrog.io`
matches no request, so the upload is sent without credentials and fails with `401`.
{{< /callout >}}

## Existing files at the upload path

When a file is already stored at the upload path, the uploader decides by its content
and owner properties:

- **Same content:** the stored file is reused.
- **Owner properties name the same resource of the same component version:** the file
  is replaced. This is a repeated transfer.
- **Anything else:** the transfer fails and asks for a different `path`.

The property names are listed in the
[reference]({{< relref "docs/reference/transfer-configuration/artifactory-uploader.md#owner-properties" >}}).

## Troubleshooting

### Symptom: `failed detecting the type of … repository …: GET … returned status 401`

**Cause:** No credentials were found for the server, so the uploader asked anonymously.

**Fix:** Configure credentials as shown above, with the bare host name in `hostname`.

### Symptom: `failed detecting the type of … repository …: GET … returned status 403`

**Cause:** The user may deploy but not read the repository settings.

**Fix:** Grant read access to the repository configuration, or use a user that has it.

### Symptom: `… already stores a file that was not uploaded for this resource …; refusing to overwrite it, configure a different path`

**Cause:** Artifactory stores a file at the upload path that belongs to another
resource, component version or was not uploaded by OCM.

**Fix:** Configure a `path` that is unique for the resource, or remove the stored file.

## Related documentation

- [How-to: Upload Resources to Sonatype Nexus]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/_index.md" >}})
- [Reference: JFrog Artifactory Uploader]({{< relref "docs/reference/transfer-configuration/artifactory-uploader.md" >}})
- [Reference: Transfer Configuration]({{< relref "docs/reference/transfer-configuration/_index.md" >}})
- [Tutorial: Configure Custom Uploads During Transfer]({{< relref "docs/tutorials/configure-custom-uploads.md" >}})
- [Concept: Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}})
- [Credential Consumer Identities]({{< relref "docs/reference/credential-consumer-identities.md" >}})
