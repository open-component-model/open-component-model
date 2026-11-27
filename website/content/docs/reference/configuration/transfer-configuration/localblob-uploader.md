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
converted to OCI via `GetHelmChart` → `ConvertHelmToOCI`, and wget, S3 and
GitHub resources are downloaded through their access-specific downloaders.

The conversion gives a Helm chart a new digest. When a signature, or the reference
digest in a parent component version, covers a recorded chart digest
(`genericBlobDigest/v1`, `SHA-256`), the chart is not converted: `GetHelmChart`
fetches it and it is stored as the chart archive (`.tgz`), with the media type
`application/vnd.cncf.helm.chart.content.v1.tar+gzip`. Its digest stays unchanged
and the provenance file is not stored. See
[Helm charts]({{< relref "docs/guides/transfer/upload-oci-images.md#charts-covered-by-a-signature" >}})
for the full rule.

Selecting an access type the local blob uploader cannot handle fails the transfer
with `local blob uploader cannot copy access type …`. Copying a Helm chart whose
digest would change fails the transfer of a component version that is signed, or
referenced by digest, with `cannot copy the Helm chart of …`.

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
