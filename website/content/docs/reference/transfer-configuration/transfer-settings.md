---
title: "Transfer Settings"
description: "Reference for transfer.config.ocm.software/v1alpha1: recursion, copy mode and upload type."
weight: 1
toc: true
---

The `transfer.config.ocm.software/v1alpha1` type controls the global transfer
behaviour. All fields are optional; when omitted they resolve to their defaults.

## Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/Config.schema.json" >}}

## Fields

| Field        | Type        | Default            | Description                                                                     |
|--------------|-------------|--------------------|---------------------------------------------------------------------------------|
| `recursive`  | `-1` or `0` | `0` (no recursion) | `-1` transfers the whole reference tree; `0` transfers only the named version.  |
| `copyMode`   | enum        | `localBlob`        | Which resources are copied. See [Copy Mode](#copy-mode).                        |
| `uploadType` | enum        | `localBlob`        | How copied resources are stored in the target. See [Upload Type](#upload-type). |

### Copy Mode

| Value          | Meaning                                                                                                               |
|----------------|-----------------------------------------------------------------------------------------------------------------------|
| `localBlob`    | Copy only resources already stored as local blobs. External resources keep their original access and are not fetched. |
| `allResources` | Fetch every external resource and re-upload it to the target. Equivalent to the CLI `--copy-resources` flag.          |

### Upload Type

Only relevant when a resource is being copied.

| Value         | Meaning                                                                                                     |
|---------------|-------------------------------------------------------------------------------------------------------------|
| `localBlob`   | Embed the resource content as a local blob in the target component version's manifest. This is the default. |
| `ociArtifact` | Upload the resource as a separate OCI artifact in the target registry (OCI targets only; `--upload-as`).    |
