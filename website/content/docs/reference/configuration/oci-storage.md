---
title: "OCI Storage"
description: "Reference for oci.storage.config.ocm.software/v1alpha1: the storage layout of component versions written to OCI registries and CTF archives."
icon: "📦"
weight: 6
toc: true
---

The `oci.storage.config.ocm.software/v1alpha1` type selects how the OCM CLI stores component versions in OCI
registries and CTF archives. It applies to every command that writes a component version: `ocm add
component-version`, `ocm transfer component-version`, and `ocm sign component-version`.

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: oci.storage.config.ocm.software/v1alpha1
    componentVersionLayout: normalized
```

## Fields

| Field                    | Type                      | Default   | Description                                                       |
|--------------------------|---------------------------|-----------|-------------------------------------------------------------------|
| `componentVersionLayout` | `default` or `normalized` | `default` | Layout of every component version written to OCI and CTF targets. |

- `default`: the `v2` layout. The version tag points to an OCI manifest that contains the full component descriptor.
- `normalized` (experimental): the version tag points to a manifest that contains only the normalized component descriptor. Its
  digest is identical in every registry, so you can sign it with `cosign` or `notary`. See
  [Signatures as OCI Referrers]({{< relref "docs/concepts/transfer-concept.md" >}}#signatures-as-oci-referrers-normalized-layout).

Any other value makes the CLI fail. If the configuration contains more than one entry, the last
`componentVersionLayout` that is set wins. There is no command-line flag; pass the entry with `--config`, also from
stdin (`--config -`).

## Behavior

- **Reading** detects the layout of each component version, so a repository can contain both layouts, and a
  normalized component version can reference a `v2` one.
- **Writing** always uses the configured layout. OCM does not keep the layout of a version that already exists.
- **Signing** with `ocm sign component-version` persists the signature by writing the component version again, so it
  is rewritten in the configured layout. Without `componentVersionLayout: normalized`, signing a normalized component
  version converts it to `v2`: its tag moves off the normalized manifest, and the cosign or notary signatures
  attached to that manifest are no longer reachable from the tag. With the normalized layout configured, the
  normalized manifest and its signatures stay unchanged.
- **Transfer** writes every component version in the configured layout. For a normalized source, the transfer copies
  the OCI referrers of its normalized manifest, such as cosign signatures, to the target. If the target is not
  written in the normalized layout, they are not copied and a warning is logged.

## Schema

The schema below defines the full structure of the `oci.storage.config.ocm.software/v1alpha1` type as specified by
[JSON Schema 2020-12](https://json-schema.org/draft/2020-12/schema).

---

{{< schema-renderer url="/schemas/bindings/go/oci/storage/v1alpha1/Config.schema.json" >}}
