---
title: "Local Blob Uploader"
description: "Reference for localblob.uploader.transfer.config.ocm.software/v1alpha1: download matched resources and embed them as local blobs."
weight: 3
toc: true
---

Downloads a selected resource and embeds it in the target as a local blob. This
is the same operation the baseline applies to local blobs, extended to any
supported access type: OCI images are fetched via `GetOCIArtifact`, Helm charts
converted to OCI via `GetHelmChart` → `ConvertHelmToOCI`, and wget, S3 and
GitHub resources are downloaded through their access-specific downloaders.

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
match: resource.access.isType(["LocalBlob", "OCIImage", "Helm", "Wget", "S3/v2", "GitHub", "Git"])
```

The default selects the access types the uploader can handle. An explicit `match`
replaces the default entirely.

A plain entry with no fields is a catch-all that copies every supported resource:

```yaml
- type: localblob.uploader.transfer.config.ocm.software/v1alpha1
```
