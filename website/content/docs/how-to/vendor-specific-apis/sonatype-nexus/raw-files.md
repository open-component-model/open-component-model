---
title: "Upload Raw Files to Sonatype Nexus"
slug: "raw-files"
description: "Upload any resource as a plain file into a Sonatype Nexus raw hosted repository during transfer and publish it as Wget/v1."
weight: 4
toc: true
---

## Goal

Transfer a component version and upload its resources as plain files into a Nexus
raw hosted repository, so that consumers download them by URL.

## You'll end up with

- Each resource stored as a file in the Nexus raw repository `raw-hosted`
- Transferred resources with a `Wget/v1` access on the stored files

**Estimated time:** ~10 minutes

## Prerequisites

- The setup in [Sonatype Nexus]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/_index.md#prerequisites" >}}),
  with [credentials configured]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/_index.md#configure-credentials" >}})
- A Nexus raw hosted repository, here `raw-hosted`
- `curl`

## Steps

### Configure the uploader

Add the credentials and one uploader rule for all `LocalBlob` resources to your
`.ocmconfig` in the working directory (merged with `$HOME/.ocmconfig`):

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
  - type: nexus.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("LocalBlob")
    url: https://nexus.example.com
    repository: raw-hosted
    # optional, defaults to <component>/<component version>/<resource>-<resource version>
    path: '${"files/" + resource.name + ".txt"}'
```

### Run the transfer

```bash
ocm transfer cv ctf::./src//ocm.software/demo:1.0.0 ctf::./target
```

### Verify

```bash
ocm get cv ctf::./target//ocm.software/demo:1.0.0 -o yaml
```

The `notes` resource has this access:

```yaml
access:
  type: Wget/v1
  url: https://nexus.example.com/repository/raw-hosted/files/notes.txt
  mediaType: text/plain
```

Download the file:

```bash
curl -fsSL -u <USERNAME>:<PASSWORD_OR_USER_TOKEN> https://nexus.example.com/repository/raw-hosted/files/notes.txt
```

## How the repository behaves

- Any access type is accepted and the content is stored as is.
- OCI images are uploaded as one gzipped OCI layout tar
  (`application/vnd.ocm.software.oci.layout.v1+tar+gzip`).
- A stored file is never overwritten, see
  [Existing files at the upload path]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/_index.md#existing-files-at-the-upload-path" >}}).
  A file with different content fails the transfer:

  ```text
  nexus repository "raw-hosted" already stores a different file at …; the uploader never overwrites files in raw repositories, configure a different path
  ```

## Troubleshooting

For credential and overwrite errors, see
[Troubleshooting]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/_index.md#troubleshooting" >}}).

## Related documentation

- [How-to: Sonatype Nexus]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/_index.md" >}})
- [How-to: Upload Generic Files to JFrog Artifactory]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/generic-files.md" >}})
- [Reference: Sonatype Nexus Uploader]({{< relref "docs/reference/transfer-configuration/nexus-uploader.md#repository-types" >}})
- [Sonatype: Raw Repositories](https://help.sonatype.com/en/raw-repositories.html)
