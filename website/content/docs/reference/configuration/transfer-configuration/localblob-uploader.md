---
title: "Local Blob Uploader"
description: "Reference for localblob.uploader.transfer.config.ocm.software/v1alpha1: download matched resources and embed them as local blobs."
weight: 3
toc: true
aliases:
  - /docs/reference/transfer-configuration/localblob-uploader/
  - /docs/reference/transfer-configuration/local-blob-uploader/
---

Downloads a selected resource and embeds it in the target as a local blob. This
is the same operation the baseline applies to local blobs, extended to any
supported access type: OCI images are streamed via `TransferOCIArtifactToOCILocalBlob`
(OCI registry target) or `TransferOCIArtifactToCTFLocalBlob` (CTF target), Helm charts
converted to OCI via `GetHelmChart` → `ConvertHelmToOCI`, and wget, S3, Git and
GitHub resources are downloaded through their access-specific downloaders. Not
every access type is streamed; see [Temporary Disk Usage](#temporary-disk-usage).

Selecting an access type the local blob uploader cannot handle fails the transfer
with `local blob uploader cannot copy access type …`.

## Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/LocalBlobUploaderConfig.schema.json" >}}

## Fields

| Field   | Type           | Required | Description                                                                                                                                                                                                                                           |
| ------- | -------------- | -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `match` | CEL expression | no       | A CEL boolean expression selecting the resources this uploader handles (`resource`, `component` and `target` are available; test access types with `resource.access.isType`). When omitted, the default below applies; an explicit value replaces it. |

## Default `match`

```yaml
match: resource.access.isType(["LocalBlob", "OCIImage", "OCIImageLayer", "Helm", "Wget", "S3/v2", "GitHub", "Git"])
```

The default selects the access types the uploader can handle. An explicit `match`
replaces the default entirely.

A plain entry with no fields is a catch-all that copies every supported resource:

```yaml
- type: localblob.uploader.transfer.config.ocm.software/v1alpha1
```

## Temporary Disk Usage

Some access types are streamed from the source into the target. The others are
first downloaded to a temporary file on the machine that runs the transfer:

| Source access                   | Copied as local blob                                   |
| ------------------------------- | ------------------------------------------------------ |
| `LocalBlob`                     | Streamed, no temporary file                            |
| `OCIImage`, `OCIImageLayer`     | Streamed, no temporary file                            |
| `Wget`, `S3`                    | Downloaded to a temporary file, then uploaded          |
| `Helm`                          | Downloaded and converted to OCI in temporary files     |
| `Git`, `GitHub`                 | Downloaded to a temporary file, then uploaded          |
| `relativeOciReference` (OCM v1) | Downloaded to a temporary file, then uploaded          |

This applies to OCI registry and CTF targets alike. Temporary files are removed
in a single `FileCleanup` step after all other steps of the transfer have
finished. Plan free disk space for the **sum** of all resources in the transfer
that are downloaded to a temporary file, not only for the largest one.

Repeating a transfer does not upload a streamed local blob again when the
target already stores it. A resource that goes through a temporary file is
downloaded from its source again on every run.

To move `Wget` or `S3` content without a temporary file, keep the resource by
reference and re-host it with the
[HTTP uploader]({{< relref "docs/reference/configuration/transfer-configuration/http-uploader.md" >}}),
which streams from the source to the upload target.
