---
title: "Transfer Settings"
description: "Reference for transfer.config.ocm.software/v1alpha1: global transfer settings."
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
