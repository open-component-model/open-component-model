---
title: "Reference Uploader"
description: "Reference for reference.uploader.transfer.config.ocm.software/v1alpha1: keep matched resources by reference with no transformation."
weight: 4
toc: true
---

Keeps a selected resource by reference: no transformation is applied and the
resource's access is unchanged in the target. This is the same as what the
baseline does for non-local-blob resources, but expressed as an explicit uploader
entry so it can be placed before a catch-all to exclude specific resources from
copying.

Selecting a local blob fails the transfer with
`local blobs cannot be kept by reference`.

## Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/ReferenceUploaderConfig.schema.json" >}}

## Fields

| Field   | Type           | Required | Description                                                                                                                                                                                                                                           |
| ------- | -------------- | -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `match` | CEL expression | no       | A CEL boolean expression selecting the resources this uploader handles (`resource`, `component` and `target` are available; test access types with `resource.access.isType`). When omitted, the default below applies; an explicit value replaces it. |

## Default `match`

```yaml
match: '!resource.access.isType("LocalBlob")'
```

The default excludes local blobs because selecting a local blob is an error.
An explicit `match` replaces the default entirely.
