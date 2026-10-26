---
title: "Transfer Settings"
description: "Reference for transfer.config.ocm.software/v1alpha1: global transfer settings."
weight: 1
toc: true
aliases:
  - /docs/reference/transfer-configuration/transfer-settings/
---

The `transfer.config.ocm.software/v1alpha1` type controls the global transfer
behaviour. All fields are optional; when omitted they resolve to their defaults.

## Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/Config.schema.json" >}}

## Fields

| Field                    | Type                 | Default              | Description                                                                                                              |
|--------------------------|----------------------|----------------------|--------------------------------------------------------------------------------------------------------------------------|
| `recursive`              | `-1` or `0`          | `0` (no recursion)   | `-1` transfers the whole reference tree; `0` transfers only the named version.                                           |
| `componentVersionLayout` | `v2` or `normalized` | layout of the source | Layout of component versions written to OCI and CTF targets; overrides the target repository's `componentVersionLayout`. |

When neither the transfer configuration nor the target repository sets a
layout, a component version keeps the layout of its source; other sources are
written as `v2`. With the `normalized` layout, the transfer also copies the OCI
referrers of the source component version, such as cosign signatures and
attestations, to the target. `ocm transfer component-version --layout` sets
this field. See
[Signatures Across Boundaries]({{< relref "docs/concepts/transfer-concept.md" >}}#signatures-across-boundaries).
