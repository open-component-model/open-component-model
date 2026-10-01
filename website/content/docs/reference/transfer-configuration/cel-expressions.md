---
title: "CEL Expressions"
description: "Reference for the CEL expressions in match, target URL, header, path and imageReference fields of uploaders."
weight: 8
toc: true
---

Uploader fields use two forms of [CEL](https://cel.dev/) expressions:

- **`match`** is a plain CEL boolean expression (no `${…}` wrapper). It is
  evaluated once per resource while the transfer graph is built.
- **`targetURL`**, every **`header`** value, the **`path`** of the Artifactory
  and Nexus uploaders, and the **`imageReference`** of the OCI uploader are CEL
  templates: a CEL value **must be wrapped in `${…}`**. A value with no `${…}`
  is a plain literal and is used verbatim.

All expressions are evaluated against the source resource and resolved by the
transfer runtime, so the produced plan is deterministic. CEL string concatenation
(`+`), conditionals (`cond ? a : b`), and comparisons are all available.

## Identifiers

### `resource`

The `resource` alias always exposes:

| Expression                               | Value                                                        |
| ---------------------------------------- | ------------------------------------------------------------ |
| `resource.name`                          | Resource name.                                               |
| `resource.version`                       | Resource version.                                            |
| `resource.type`                          | Resource type.                                               |
| `resource.extraIdentity.<key>`           | A value from the resource's extra identity.                  |
| `resource.labels`                        | The resource labels as a list of `{name, value}` objects.    |
| `resource.digest.value`                  | The source digest value, when the resource carries a digest. |
| `resource.digest.hashAlgorithm`          | The source digest hash algorithm (e.g. `SHA-256`).           |
| `resource.digest.normalisationAlgorithm` | The source digest normalisation algorithm.                   |

Every field of the **source access** is exposed dynamically under
`resource.access.<field>`, so the expression works with any access type — the
field names are exactly those of that access. For example:

| Source access | Available under `resource.access` |
| ------------- | --------------------------------- |
| `Wget/v1`     | `url`, `mediaType`                |
| `OCIImage/v1` | `imageReference`                  |
| `S3/v1`       | `bucket`, `key`, `region`, …      |

### `component`

The `component` alias exposes the source component version:

| Expression           | Value                     |
| -------------------- | ------------------------- |
| `component.name`     | Component name.           |
| `component.version`  | Component version.        |
| `component.provider` | Component provider name.  |

### `target`

The `target` alias exposes the transfer target as a map:

| Target type  | Fields                                                  |
| ------------ | ------------------------------------------------------- |
| OCI registry | `{"type": "OCIRepository", "baseUrl": …, "subPath": …}` |
| CTF archive  | `{"type": "CommonTransportFormat", "filePath": …}`      |

`target` is available in `match` expressions and in `${…}` templates.

## Functions

### `isType`

`resource.access.isType(arg)` tests the access type against the argument, with
alias and version resolution. See
[`isType` semantics]({{< relref "docs/reference/transfer-configuration/_index.md#istype-semantics" >}})
on the overview page.

- `resource.access.isType(string)` — true when the access type matches.
- `resource.access.isType(list(string))` — true when any element matches.

### `isOCIManifest`

`isOCIManifest(string)` returns true for OCI image manifest and index, and
Docker manifest and manifest list media types.

### `toOCI`

`resource.access.toOCI()` returns a map with keys `host`, `registry`, `repository`,
`tag`, `digest`, `reference` for OCI image accesses.

### `url`

`url(<string>)` (also callable as `<string>.url()`) parses a URL string and
returns a map with the string keys `scheme`, `host`, `hostname`, `port`, `path`,
`rawPath`, `rawQuery`, `fragment`, and `user`. For a `Wget/v1` source,
`url(resource.access.url).path` yields the path of the source URL.

### String extensions

`split`, `join`, `endsWith`, `startsWith`, `contains`, `replace`, `trim`, … from
the [CEL string extensions](https://github.com/google/cel-go/blob/master/ext/README.md).

### Checksum functions

`contentDigestAlgorithm(<string>)` maps an OCM digest algorithm name to its RFC 9530
`Content-Digest`/`Repr-Digest` key (`SHA-256` → `sha-256`); `hex.decode`/`hex.encode`
and the CEL encoders extension's `base64.encode`/`base64.decode` convert a hex digest
to the base64 value those fields expect. See
[Templating Headers]({{< relref "docs/reference/transfer-configuration/http-uploader.md#templating-headers" >}}).

## Expression scope

Because the field set is derived from the matched resource's own access, an
expression may only reference fields that exist on every resource the uploader
matches — scope the rule with `match` so all matched resources share a shape.
Referencing a field that is absent at execution time fails the transfer with a
clear error rather than producing a partial URL.

## Examples

```yaml
# Preserve the source path under a new host
targetURL: '${"https://mytarget.example.com" + url(resource.access.url).path}'

# Route by name and version
targetURL: '${"https://cdn.example.com/" + resource.name + "/" + resource.version + "/blob"}'

# Use extra identity attributes
targetURL: '${"https://" + resource.extraIdentity.region + ".example.com/" + resource.extraIdentity.arch + url(resource.access.url).path}'

# Conditional target driven by an extra-identity attribute
targetURL: '${resource.extraIdentity.tier == "public" ? "https://cdn.example.com" + url(resource.access.url).path : "https://internal.example.com" + url(resource.access.url).path}'

# Non-wget source: reference an access-specific field (OCI imageReference)
targetURL: '${"https://mirror.example.com/" + resource.access.imageReference}'

# Use the component name in the path
path: '${component.name + "/" + component.version + "/" + resource.name}'
```
