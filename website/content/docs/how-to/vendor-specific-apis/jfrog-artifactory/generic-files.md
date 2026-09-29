---
title: "Upload Generic Files to JFrog Artifactory"
slug: "generic-files"
description: "Upload any resource as a plain file into a JFrog Artifactory generic repository during transfer and publish it as Wget/v1."
weight: 4
toc: true
---

## Goal

Transfer a component version and upload its resources as plain files into an
Artifactory generic repository, so that consumers download them by URL.

## You'll end up with

- Each resource stored as a file in the Artifactory generic repository `generic-local`
- Transferred resources with a `Wget/v1` access on the stored files

**Estimated time:** ~10 minutes

## Prerequisites

- The setup in [Upload Resources to JFrog Artifactory]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/_index.md#prerequisites" >}}),
  with [credentials configured]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/_index.md#configure-credentials" >}})
- An Artifactory generic repository, here `generic-local`
- `curl`

## Steps

### Configure the uploader

Create `config.yaml` with the credentials and one uploader rule for all `localBlob` resources:

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
  - type: artifactory.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: localBlob
    url: https://myorg.jfrog.io
    repository: generic-local
    # The default path written out, a starting point for your own layout:
    # path: '${component.name + "/" + component.version + "/" + resource.name + "-" + resource.version}'
```

### Run the transfer

```bash
ocm transfer cv --config config.yaml ctf::./src//ocm.software/demo:1.0.0 ctf::./target
```

### Verify

```bash
ocm get cv ctf::./target//ocm.software/demo:1.0.0 -o yaml
```

The `payload` resource has this access:

```yaml
access:
  type: Wget/v1
  url: https://myorg.jfrog.io/artifactory/generic-local/ocm.software/demo/1.0.0/payload-1.0.0
  mediaType: text/plain
```

Download the file:

```bash
curl -fsSL -H "Authorization: Bearer <ARTIFACTORY_IDENTITY_TOKEN>" https://myorg.jfrog.io/artifactory/generic-local/ocm.software/demo/1.0.0/payload-1.0.0
```

## How the repository behaves

- Any access type is accepted and the content is stored as is.
- OCI images are uploaded as one OCI layout tar
  (`application/vnd.ocm.software.oci.layout.v1+tar`).
- A second transfer reuses the stored file: `reused content already stored in the artifactory repository`.

## Troubleshooting

For credential and overwrite errors, see
[Troubleshooting]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/_index.md#troubleshooting" >}}).

## Related documentation

- [How-to: Upload Resources to JFrog Artifactory]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/_index.md" >}})
- [How-to: Upload Raw Files to Sonatype Nexus]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/raw-files.md" >}})
- [Reference: JFrog Artifactory Uploader]({{< relref "docs/reference/transfer-configuration/artifactory-uploader.md#repository-types" >}})
