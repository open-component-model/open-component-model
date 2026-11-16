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
(OCI registry target) or `TransferOCIArtifactToCTFLocalBlob` (CTF target), `Wget` and `S3`
content via `TransferWgetTo{OCI,CTF}LocalBlob` and `TransferS3To{OCI,CTF}LocalBlob`, Helm
charts are converted to OCI via `GetHelmChart` → `ConvertHelmToOCI`, and Git and GitHub
resources are downloaded through their access-specific downloaders. Not every path is
free of temporary files; see [Disk Usage During Transfer](#disk-usage-during-transfer).

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

## Disk Usage During Transfer

OCM streams content from the source into the target where it can. Where it cannot, it
writes the content to disk first. This section tells which resources need disk space,
where, and for how long.

| Source access                                   | Copied as local blob                                       |
| ----------------------------------------------- | ---------------------------------------------------------- |
| `LocalBlob`                                     | Streamed                                                   |
| `OCIImage`, `OCIImageLayer`                     | Streamed                                                   |
| `Wget` with `GET`, `S3`                         | Streamed; [spooled](#spooled-blobs) in the cases below     |
| `Wget` with another verb                        | [Buffered in a file](#buffered-files) until the end        |
| `Helm`, `Git`, `GitHub`                         | [Buffered in files](#buffered-files) until the end         |
| `relativeOciReference` (OCM v1)                 | [Buffered in a file](#buffered-files) until the end        |

Repeating a transfer does not download or upload a streamed resource again when the
target already stores its content. This needs the digest of the resource and, for a CTF
target, the size of the content before it is read. A buffered resource is downloaded
again on every run.

### Spooled Blobs

An OCI registry needs the size and the digest of a blob before the upload starts. OCM
**spools** a blob to a temporary file to learn them when one is missing:

- the server does not announce the size of a `Wget` download (no `Content-Length`, or it
  refuses `HEAD`), or
- the resource has no `digest`.

A CTF target never spools: the content is written into the archive directly.

Each spooled blob is reported twice, when spooling starts and when it is done:

```text
level=INFO msg="spooling blob to disk before upload" unknown=size directory=/var/cache/ocm resource="name=models,version=1.0.0"
level=INFO msg="spooled blob to disk" unknown=size directory=/var/cache/ocm resource="name=models,version=1.0.0" size=33554432 duration=128.433696ms
```

`unknown` tells what was missing (`size`, `digest`, or `size and digest`), `directory`
where the blob is spooled, and `size` how many bytes it took.

- **Where:** the temporary folder, set with `--temp-folder` or the `tempFolder` field of the
  filesystem configuration; see
  [Temporary folder]({{< relref "docs/reference/configuration/caching.md" >}}). OCM creates
  the folder if it does not exist.
- **How long:** only while that blob is uploaded. The space is freed when its upload ends,
  whether it succeeded or failed.
- **How much:** one file per blob that is being uploaded at that moment. Plan for the
  largest spooled blobs times the number of parallel transfers (`--concurrency-limit`),
  not for the sum of all resources.
- **Content check:** the content is verified against the `digest` of the resource while it
  is written. A mismatch fails the transfer before anything is uploaded.

On Linux and macOS the spool file has no name while it is in use. It does not appear in
a listing of the folder (`ls`, `du`), but its space counts as used (`df`). In return the
space is freed even when OCM is interrupted or killed. On Windows the file is visible,
and an OCM process that is killed leaves it behind; it is safe to delete
`ocm-blob-*` files when no transfer is running.

To avoid spooling, give the resource a `digest` and serve it from a server that
announces `Content-Length`.

### Buffered Files

The remaining access types are downloaded to a temporary file, and a Helm chart is
converted to OCI in a second one, before the upload starts. These files are created in
the temporary directory of the operating system (`$TMPDIR` on Linux and macOS); they do
not follow `tempFolder`. They are removed in a single `FileCleanup` step after all other
steps of the transfer have finished. Plan free disk space there for the **sum** of all
buffered resources in the transfer.
