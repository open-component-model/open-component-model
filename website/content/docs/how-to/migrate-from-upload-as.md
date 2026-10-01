---
title: "Migrate from --upload-as to Uploader Configurations"
slug: "migrate-from-upload-as"
description: "Replace the deprecated --upload-as flag and the removed upload type transfer setting with the oci.uploader.transfer.config.ocm.software uploader configuration."
weight: 12
toc: true
---

## Goal

Replace `--upload-as` (CLI) and `uploadType` (`transfer.config.ocm.software/v1alpha1`,
CLI config and controller `Replication` configs) with the
`oci.uploader.transfer.config.ocm.software/v1alpha1` uploader configuration.

{{< callout context="caution" >}}
The `--upload-as` and `--copy-resources` flags are deprecated. They still work:
the CLI translates them into uploader
configuration entries appended after all configured entries (see
[Deprecated flags](#deprecated-flags)). A leftover `uploadType` field in a
transfer config fails config loading with `unknown field "uploadType"` and must
be migrated before upgrading.
{{< /callout >}}

## Why Migrate?

The `--upload-as` flag applied to every resource uniformly — all resources were
either uploaded as OCI artifacts or stored as local blobs. An uploader
configuration matches per resource (`match` CEL expression), controls the target
location (`imageReference`), and shares one mechanism with the HTTP uploader.

## Prerequisites

- [OCM CLI]({{< relref "/docs/getting-started/ocm-cli-installation.md" >}}) installed (the version that deprecated `--upload-as`)
- An existing workflow that uses `--upload-as` or `uploadType`

## Deprecated flags

Until you migrate, the CLI translates the deprecated flags into uploader
configuration entries. The translated entries come after all entries from the
OCM configuration, so explicit rules still win.

To migrate, add the entries from the table below to your OCM configuration (for
example `.ocmconfig` in your working directory, which is merged with your other
configuration files) and drop the deprecated flags.

The following table shows what each deprecated flag combination translates to:

| Deprecated flags | Generated configuration entries |
| --- | --- |
| `--copy-resources` | `- type: localblob.uploader.transfer.config.ocm.software/v1alpha1` |
| `--copy-resources --upload-as ociArtifact` | `- type: oci.uploader.transfer.config.ocm.software/v1alpha1` then `- type: localblob.uploader.transfer.config.ocm.software/v1alpha1` |
| `--upload-as ociArtifact` | `- type: oci.uploader.transfer.config.ocm.software/v1alpha1` with `match: 'target.type == "OCIRepository" && resource.access.isType("LocalBlob") && has(resource.access.mediaType) && isOCIManifest(resource.access.mediaType) && has(resource.access.referenceName)'` |
| `--upload-as localBlob`, `--copy-resources=false` | nothing |

`--copy-resources=false` used to override `copyMode: allResources` from a
config. It has no effect now: a flag cannot remove uploader entries configured in
the OCM configuration.

## Steps

The table below maps every old usage to its replacement. Each row is followed by
a Before / After pair showing the exact change.

| Before | After | Behaviour difference |
| -------- | ------- | ---------------------- |
| `--upload-as localBlob`, `uploadType: localBlob`, or nothing | Drop the flag/field. | None. Local blob is the default. |
| `--copy-resources --upload-as ociArtifact`, or `copyMode: allResources` + `uploadType: ociArtifact` | Add an `oci.uploader.transfer.config.ocm.software/v1alpha1` entry and a `localblob.uploader.transfer.config.ocm.software/v1alpha1` catch-all entry in a config file. | None. Same target references: `<baseUrl>[/<subPath>]/<repository>[:<tag>]`. |
| `--upload-as ociArtifact` without `--copy-resources` (only OCI-manifest local blobs became OCI artifacts; OCI image / Helm references stayed by reference) | One OCI uploader entry whose `match` selects only OCI-manifest local blobs with a `referenceName` (see step 3). | None. |
| Controller: `uploadType: ociArtifact` in the transfer config referenced by a `Replication` | Remove `uploadType` from that config entry; add the OCI uploader entry to the same config (ConfigMap/Secret). | None. The controller reads uploader entries from the same configs (`LookupUploaderConfigs`). |

### The default path mapping

`--upload-as ociArtifact` placed every artifact at the hard-coded path
`<target baseUrl>[/<subPath>]/<name>`. The default
`imageReference` produces the same layout, so you do not need to write one
(the full template is in the
[`imageReference` reference]({{< relref "docs/reference/transfer-configuration/oci-uploader.md#imagereference" >}})).

The name component depends on the access type:

- **Local blob** (OCI manifest media type): `access.referenceName` verbatim. E.g. `ghcr.io/org/image:v1` → `<target>/ghcr.io/org/image:v1`.
- **Helm**: repository URL path + chart name, tagged with version. E.g. `https://stefanprodan.github.io/podinfo` + `podinfo:6.5.0` → `podinfo/podinfo:6.5.0`.
- **OCI image**: `resource.access.toOCI().repository` + tag. E.g. `ghcr.io/org/image:v1` → `org/image:v1`.

The references match the old `--upload-as ociArtifact` flag for all three access types.

- `target` is the OCI registry target: `target.baseUrl` is the registry host,
  `target.subPath` is the repository prefix (e.g. for target `ghcr.io/target-org/ocm`,
  `baseUrl` is `ghcr.io` and `subPath` is `target-org/ocm`).

A plain `oci.uploader.transfer.config.ocm.software/v1alpha1` entry with no
fields uses exactly this mapping.

### Which resources the OCI uploader selects

An uploader handles the resources its `match` selects. `match` is a CEL boolean
expression. Without an explicit `match`, the OCI uploader uses this default:

```yaml
match: >-
  target.type == "OCIRepository"
  && (resource.access.isType(["OCIImage", "Helm"])
    || (resource.access.isType("LocalBlob")
      && has(resource.access.mediaType) && isOCIManifest(resource.access.mediaType)
      && has(resource.access.referenceName)))
```

This is exactly what `--upload-as ociArtifact` uploaded. `resource.access.isType`
resolves aliases, so `isType("OCIImage")` matches `ociArtifact/v1`,
`ociImage/v1`, etc. An explicit `match` replaces the default. See
[`match`]({{< relref "docs/reference/transfer-configuration/_index.md#match" >}})
for all identifiers and functions.

### 1. Local blob (default) — drop the flag

{{< tabs "migration-local-blob" >}}
{{< tab "Before" >}}

```bash
ocm transfer cv --copy-resources --upload-as localBlob <src> <target>
```

Or in config:

```yaml
- type: transfer.config.ocm.software/v1alpha1
  copyMode: allResources
  uploadType: localBlob
```

{{< /tab >}}
{{< tab "After" >}}

`.ocmconfig` in the working directory:

> The CLI merges `.ocmconfig` from the current directory with your other OCM configuration (such as `$HOME/.ocmconfig`), so credentials and resolvers stay in effect. Passing a file with `--config` would replace that configuration instead.

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
```

```bash
ocm transfer cv <src> <target>
```

The local-blob uploader catch-all replaces `--copy-resources` and
`copyMode: allResources`. If the old command did not use `--copy-resources`
(only `--upload-as localBlob`), omit the catch-all: local blobs are copied by
default and no uploader entry is needed.

{{< /tab >}}
{{< /tabs >}}

### 2. OCI artifact (with `--copy-resources`) — add an OCI uploader entry

{{< tabs "migration-oci-artifact" >}}
{{< tab "Before" >}}

```bash
ocm transfer cv --copy-resources --upload-as ociArtifact <src> <target>
```

Or in config:

```yaml
- type: transfer.config.ocm.software/v1alpha1
  copyMode: allResources
  uploadType: ociArtifact
```

{{< /tab >}}
{{< tab "After" >}}

`.ocmconfig` in the working directory:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
```

```bash
ocm transfer cv <src> <target>
```

The default `imageReference` gives the same target references as
before; set `imageReference` only to place artifacts elsewhere.

{{< /tab >}}
{{< /tabs >}}

### 3. OCI artifact without `--copy-resources` — match local blobs only

{{< tabs "migration-oci-local-blob-only" >}}
{{< tab "Before" >}}

```bash
ocm transfer cv --upload-as ociArtifact <src> <target>
```

Without `--copy-resources`, only OCI-manifest local blobs became OCI artifacts.
OCI image and Helm references stayed by reference.

{{< /tab >}}
{{< tab "After" >}}

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: >-
      target.type == "OCIRepository"
      && resource.access.isType("LocalBlob")
      && has(resource.access.mediaType) && isOCIManifest(resource.access.mediaType)
      && has(resource.access.referenceName)
```

`resource.access.isType` resolves the `LocalBlob` / `localBlob` spellings, so
one entry is enough.

{{< /tab >}}
{{< /tabs >}}

### 4. Controller `Replication` — move from `uploadType` to the uploader entry

{{< tabs "migration-controller" >}}
{{< tab "Before" >}}

```yaml
# In the Secret / ConfigMap referenced by the Replication:
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    recursive: -1
    copyMode: allResources
    uploadType: ociArtifact
```

{{< /tab >}}
{{< tab "After" >}}

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    recursive: -1
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
```

Remove `uploadType` from the transfer config entry and add the OCI uploader
entry to the same config. The controller reads uploader entries from the same
configs (`LookupUploaderConfigs`).

{{< /tab >}}
{{< /tabs >}}

## Behaviour to know after migrating

- An uploader handles exactly the resources its `match` selects. An entry
  without an explicit `match` therefore also uploads OCI image and Helm
  resources (they previously stayed by reference when using the old
  `--upload-as` flag), which is why the local-blob-only row above exists.
- There is no fall-through: if a selected resource cannot be uploaded (an
  explicit `match` selects a `Wget` resource, or `imageReference` does not
  evaluate for it), the transfer fails instead of silently copying the resource
  as a local blob.
- The default `match` selects nothing on a CTF target, so resources keep the
  default handling there. To push to a registry from a CTF transfer, use a
  `match` without the target check and an absolute `imageReference`.
- A local blob's `access.referenceName` is used verbatim in the default
  template (whatever it contains, including host/port/digest).
  `ghcr.io/org/image:v1` therefore still lands at
  `<target>/ghcr.io/org/image:v1`, as with the old flag.
- A local blob that is not an OCI manifest, or has no `referenceName`, is not
  selected by the default `match`.
- Wget, S3 and GitHub resources are never OCI-uploaded.

The following table shows what the default `match` selects:

| Source access type | Selected when | Name used in the default `imageReference` |
| --- | --- | --- |
| `OCIImage` (all aliases) | OCI registry target | `resource.access.toOCI().repository` + tag. E.g. `ghcr.io/org/image:v1` → `org/image:v1` (registry and digest dropped). |
| `Helm` | OCI registry target | Helm repository URL path + chart name, tagged with version. E.g. chart `podinfo:6.5.0` from `https://stefanprodan.github.io/podinfo` → `podinfo/podinfo:6.5.0`. |
| `LocalBlob` | OCI registry target, OCI manifest media type, and a `referenceName` | `resource.access.referenceName` verbatim. E.g. `stefanprodan/podinfo:6.5.0` → `stefanprodan/podinfo:6.5.0`; `ghcr.io/org/image:v1` → `ghcr.io/org/image:v1`. |
| anything else (Wget, S3, GitHub, …) | never | — |

## Beyond the old flag

The OCI uploader configuration supports capabilities that `--upload-as` did not.

### Relocate OCI images to a custom registry path

`toOCI()` resolves OCI image accesses only, so select only those:

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
  match: target.type == "OCIRepository" && resource.access.isType("OCIImage")
  imageReference: '${"ghcr.io/target-org/images/" + resource.access.toOCI().repository + ":" + resource.access.toOCI().tag}'
```

### Relocate local blobs under a mirror

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
  match: resource.access.isType("LocalBlob") && has(resource.access.mediaType) && isOCIManifest(resource.access.mediaType) && has(resource.access.referenceName)
  imageReference: '${"ghcr.io/mirror/" + resource.access.referenceName}'
```

### Match a specific resource

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
  match: resource.name == "my-image"
  imageReference: ghcr.io/target-org/special/my-image:1.0.0
```

### CTF target with a custom registry for artifacts

The default `match` selects nothing on a CTF target, so spell out a `match`
without the target check:

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
  match: resource.access.isType("LocalBlob") && has(resource.access.mediaType) && isOCIManifest(resource.access.mediaType) && has(resource.access.referenceName)
  imageReference: '${"registry.example.com/mirror/" + resource.access.referenceName}'
```

```bash
ocm transfer cv <src> ctf::./archive
```

### Build a reference from resource metadata

When the resource has no OCI reference name, build the reference from
`resource.name` and `resource.version` instead, and select such local blobs
explicitly (the default `match` requires a `referenceName`):

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
  match: target.type == "OCIRepository" && resource.access.isType("LocalBlob") && has(resource.access.mediaType) && isOCIManifest(resource.access.mediaType)
  imageReference: '${target.baseUrl + (target.subPath == "" ? "" : "/" + target.subPath) + "/" + resource.name + ":" + resource.version}'
```

### Use a local blob's reference name as-is

The default places a local blob under the target, at
`<target>/<referenceName>`. If `referenceName` already is the full reference
you want, such as `ghcr.io/org/image:v1`, use it directly:

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
  match: resource.name == "my-image"
  imageReference: '${resource.access.referenceName}'
```

This `match` replaces the default, so the entry selects `my-image` on any target
and for any access type. If `my-image` is not a local blob with a
`referenceName` (for example an OCI image or Helm chart), the template does not
evaluate and the transfer fails.

The CEL identifiers available in `match` and `imageReference` are:

| Identifier | Value |
| --- | --- |
| `resource` | The source resource descriptor (same `resource` alias as the HTTP uploader). Fields are resolved dynamically, so an expression may use `has()` and read fields of any access type. For OCI image accesses, call `resource.access.toOCI()` to get a map with `repository`, `tag`, `host`, `digest`, etc. In transfers, `toOCI()` resolves OCI image accesses only. Test access types with `resource.access.isType(...)`. |
| `target` | The transfer target. An OCI registry has `type: OCIRepository`, `baseUrl` (registry host) and `subPath` (repository prefix; may be `""`); a CTF archive has `type: CommonTransportFormat` and `filePath`. |

## Verify the migration

Run the transfer with `--dry-run` to inspect the generated plan without writing
to the target:

```bash
ocm transfer cv --dry-run -o yaml <src> <target>
```

Check for `TransferOCIArtifact` / `AddOCIArtifact` nodes. Their `imageReference`
is your template with the identifiers replaced by their resolved values; it is
evaluated when the transfer runs.

{{< details "Expected output (TransferOCIArtifact)" >}}

For a component with an OCI image resource (`ghcr.io/stefanprodan/podinfo:6.5.0`)
transferred to `ghcr.io/target-org/ocm` with the plain OCI uploader entry
(abridged):

```yaml
- id: ...Transfer...
  label: uploader-demo@1.0.0 [Transfer image to OCI]
  spec:
    resource:
      access:
        imageReference: ghcr.io/stefanprodan/podinfo:6.5.0@sha256:ef96ad0e...
        type: OCIImage/v1
    targetResource:
      access:
        imageReference: >-
          ${{"type": "OCIRepository", "baseUrl": "ghcr.io", "subPath": "target-org/ocm"}.baseUrl
          + ...
          + "/" + environment.<id>.component.resources[0].access.toOCI().repository
          + ...}
        type: ociArtifact/v1
  type: TransferOCIArtifact/v1alpha1
```

The `target` identifier is replaced by a map literal of the target spec and
`resource` by the resource's path in the graph environment. The expression
evaluates during the transfer to `ghcr.io/target-org/ocm/stefanprodan/podinfo:6.5.0`.

For a Helm chart resource, you will see `GetHelmChart`, `ConvertHelmToOCI`, and
then `AddOCIArtifact/v1alpha1` whose `spec.resource.access.imageReference`
evaluates to `ghcr.io/target-org/ocm/podinfo/podinfo:6.5.0`.

With a relocation template and a CTF target, the dry-run shows the unevaluated
CEL expression with the `resource` identifier expanded:

```yaml
imageReference: >-
  ${"ghcr.io/mirror/"
  + environment.<id>.component.resources[0].access.toOCI().repository
  + ":" + environment.<id>.component.resources[0].access.toOCI().tag}
```

{{< /details >}}

## Troubleshooting

### Symptom: `unknown field "uploadType"`

The full error is: `looking up transfer config failed: failed to decode transfer config: type "transfer.config.ocm.software/v1alpha1" has unknown or invalid fields: json: unknown field "uploadType"`.

**Cause:** The `uploadType` field has been removed from the transfer config type.
Strict decoding now rejects unknown fields.

**Fix:** Delete the `uploadType` field from your transfer config entry and add an
`oci.uploader.transfer.config.ocm.software/v1alpha1` uploader entry instead.

### Symptom: Resources still end up as local blobs or stay by reference

**Cause:** The `match` of the uploader does not select the resource, so the resource
follows the default handling. With the default `match` this happens when:

- the target is not an OCI registry, for example a CTF archive;
- the resource is a local blob whose media type is not an OCI manifest, or that
  has no `access.referenceName`;
- the resource is a Wget, S3, GitHub or other resource the OCI uploader cannot
  upload.

**Fix:** Spell out a `match` that selects the resource. For a CTF target,
drop the target check and use an absolute registry prefix instead of `target`:

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
  match: resource.access.isType("LocalBlob") && has(resource.access.mediaType) && isOCIManifest(resource.access.mediaType) && has(resource.access.referenceName)
  imageReference: '${"ghcr.io/my-org/mirror/" + resource.access.referenceName}'
```

For a local blob without a reference name, select it and build the reference
from the resource instead:

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
  match: target.type == "OCIRepository" && resource.access.isType("LocalBlob") && has(resource.access.mediaType) && isOCIManifest(resource.access.mediaType)
  imageReference: '${target.baseUrl + (target.subPath == "" ? "" : "/" + target.subPath) + "/" + resource.name + ":" + resource.version}'
```

### Symptom: `oci uploader cannot upload access type`

The full error names the uploader and the resource, for example
`uploader 0 (oci.uploader.transfer.config.ocm.software/v1alpha1) …: oci uploader cannot upload access type Wget/v1 (adjust match)`.

**Cause:** An explicit `match` selected a resource the OCI uploader cannot
upload. The
OCI uploader uploads OCI images, Helm charts, and local blobs holding an OCI
manifest. A local blob that is not an OCI manifest fails with
`not an OCI manifest` instead.

**Fix:** Narrow `match`, e.g. add `&& resource.access.isType(["OCIImage", "Helm"])`.

### Symptom: `imageReference does not evaluate`

**Cause:** The uploader selected the resource, but its `imageReference` fails
for it, for example the default template on a CTF target (it reads
`target.baseUrl`), or `resource.access.toOCI()` on a Helm chart.

**Fix:** Narrow `match` to the resources the template works for, or use a
template that does not read the missing field.

### Symptom: `unknown field "copyMode"`

The full error is: `looking up transfer config failed: failed to decode transfer config: type "transfer.config.ocm.software/v1alpha1" has unknown or invalid fields: json: unknown field "copyMode"`.

**Cause:** The `copyMode` field has been removed from the transfer config type.
Strict decoding now rejects unknown fields.

**Fix:** Delete the `copyMode` field and apply the mapping below.

## Migrate `copyMode`

The `copyMode` field of `transfer.config.ocm.software/v1alpha1` has been removed.
The following table maps old values to their replacements:

| Old | New |
| --- | --- |
| `copyMode: localBlob` or omitted | Delete the field; nothing else (baseline). |
| `copyMode: allResources` | Delete the field; append `- type: localblob.uploader.transfer.config.ocm.software/v1alpha1` as the last uploader entry. |
| `--copy-resources` | Add `- type: localblob.uploader.transfer.config.ocm.software/v1alpha1` as the last uploader entry in a config file. |
| Exclude a resource from copying | Add a `reference.uploader.transfer.config.ocm.software/v1alpha1` entry with `match` before the catch-all. |

## Related documentation

- [Transfer Configuration Reference]({{< relref "docs/reference/transfer-configuration/_index.md" >}}) — Full configuration schema and field descriptions
- [Configure Custom Uploads During Transfer]({{< relref "docs/tutorials/configure-custom-uploads.md" >}}) — Tutorial for the HTTP streaming uploader
- [Transfer Helm Charts with OCM]({{< relref "docs/how-to/transfer-helm-charts.md" >}}) — Transfer component versions containing Helm charts
- [Replicate Component Versions with the Controller]({{< relref "docs/how-to/replicate-component-versions-controller.md" >}}) — Controller-based replication
