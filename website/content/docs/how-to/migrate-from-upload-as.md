---
title: "Migrate from --upload-as to Uploader Configurations"
slug: "migrate-from-upload-as"
description: "Replace the removed --upload-as flag and upload type transfer setting with the oci.uploader.transfer.config.ocm.software uploader configuration."
weight: 12
toc: true
---

## Goal

Replace `--upload-as` (CLI) and `uploadType` (`transfer.config.ocm.software/v1alpha1`,
CLI config and controller `Replication` configs) with the
`oci.uploader.transfer.config.ocm.software/v1alpha1` uploader configuration.

{{< callout context="caution" >}}
The `--upload-as` flag has been removed. Passing it fails with `unknown flag: --upload-as`.
A leftover `uploadType` field in a transfer config fails config loading with
`unknown field "uploadType"`. Both must be migrated before upgrading.
{{< /callout >}}

## Why Migrate?

The `--upload-as` flag applied to every resource uniformly — all resources were
either uploaded as OCI artifacts or stored as local blobs. An uploader
configuration matches per resource (`match.accessType`, `match.name`,
`match.version`, `match.extraIdentity`), controls the target location
(`imageReference`), and shares one mechanism with the HTTP uploader.

## Prerequisites

- [OCM CLI]({{< relref "/docs/getting-started/ocm-cli-installation.md" >}}) installed (the version that removed `--upload-as`)
- An existing workflow that uses `--upload-as` or `uploadType`

## Steps

The table below maps every old usage to its replacement. Each row is followed by
a Before / After pair showing the exact change.

| Before | After | Behaviour difference |
| -------- | ------- | ---------------------- |
| `--upload-as localBlob`, `uploadType: localBlob`, or nothing | Drop the flag/field. | None. Local blob is the default. |
| `--copy-resources --upload-as ociArtifact`, or `copyMode: allResources` + `uploadType: ociArtifact` | Keep `--copy-resources` / `copyMode: allResources`; add an `oci.uploader.transfer.config.ocm.software/v1alpha1` entry (no other fields needed). | None. Same target references: `<baseUrl>[/<subPath>]/<repository>[:<tag>]`. |
| `--upload-as ociArtifact` without `--copy-resources` (only OCI-manifest local blobs became OCI artifacts; OCI image / Helm references stayed by reference) | Two OCI uploader entries (no `imageReference` needed), one with `match: {accessType: LocalBlob}` and one with `match: {accessType: localBlob}`. | None. Both entries are needed because `match.accessType` compares the type name exactly, and descriptors carry either spelling. |
| Controller: `uploadType: ociArtifact` in the transfer config referenced by a `Replication` | Remove `uploadType` from that config entry; add the OCI uploader entry to the same config (ConfigMap/Secret). | None. The controller reads uploader entries from the same configs (`LookupUploaderConfigs`). |

### The default path mapping

`--upload-as ociArtifact` placed every artifact at the hard-coded path
`<target baseUrl>[/<subPath>]/<repository>[:<tag>]`. The OCI uploader default
`imageReference` produces the same layout:

```yaml
imageReference: >-
  ${target.baseUrl
  + (target.subPath == "" ? "" : "/" + target.subPath)
  + "/" + resource.access.toOCI().repository
  + (resource.access.toOCI().tag == "" ? "" : ":" + resource.access.toOCI().tag)}
```

- `target` is the OCI registry target: `target.baseUrl` is the registry host,
  `target.subPath` is the repository prefix (e.g. for target `ghcr.io/target-org/ocm`,
  `baseUrl` is `ghcr.io` and `subPath` is `target-org/ocm`).
- `resource.access.toOCI()` returns a map with `repository` and `tag` (among
  others) parsed from the source access (e.g. `ghcr.io/stefanprodan/podinfo:6.5.0`
  → repository `stefanprodan/podinfo`, tag `6.5.0`).

This is the default when `imageReference` is omitted — a plain
`oci.uploader.transfer.config.ocm.software/v1alpha1` entry with no fields is
equivalent.

{{< callout context="note" title="match.accessType does not resolve aliases" icon="outline/info-circle" >}}
`match.accessType` compares type names exactly. Access types have multiple alias
names in descriptors (e.g. OCI images: `OCIImage`, `ociArtifact`, `ociRegistry`,
`ociImage`; local blobs: `LocalBlob`, `localBlob`; Helm: `Helm`, `helm`). If you
match by access type, add one entry per alias name your descriptors carry.
Matching by `match.name` avoids this issue entirely.
{{< /callout >}}

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

```bash
ocm transfer cv --copy-resources <src> <target>
```

Or in config:

```yaml
- type: transfer.config.ocm.software/v1alpha1
  copyMode: allResources
```

No uploader entry needed. Local blob is the default when no uploader matches.

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

Create an OCM config file (e.g. `ocmconfig.yaml`):

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    imageReference: >-
      ${target.baseUrl
      + (target.subPath == "" ? "" : "/" + target.subPath)
      + "/" + resource.access.toOCI().repository
      + (resource.access.toOCI().tag == "" ? "" : ":" + resource.access.toOCI().tag)}
```

This is also the default when `imageReference` is omitted. Writing it out makes
the mapping visible and easy to change in place.

Then run:

```bash
ocm transfer cv --copy-resources --config ./ocmconfig.yaml <src> <target>
```

`--config` may be repeated, so the uploader can live in its own file.

Or add the entry to the same config that already holds the transfer settings:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    copyMode: allResources
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    imageReference: >-
      ${target.baseUrl
      + (target.subPath == "" ? "" : "/" + target.subPath)
      + "/" + resource.access.toOCI().repository
      + (resource.access.toOCI().tag == "" ? "" : ":" + resource.access.toOCI().tag)}
```

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
    match:
      accessType: LocalBlob
    imageReference: >-
      ${target.baseUrl
      + (target.subPath == "" ? "" : "/" + target.subPath)
      + "/" + resource.access.toOCI().repository
      + (resource.access.toOCI().tag == "" ? "" : ":" + resource.access.toOCI().tag)}
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: localBlob
    imageReference: >-
      ${target.baseUrl
      + (target.subPath == "" ? "" : "/" + target.subPath)
      + "/" + resource.access.toOCI().repository
      + (resource.access.toOCI().tag == "" ? "" : ":" + resource.access.toOCI().tag)}
```

Both entries are needed because `match.accessType` compares the type name
exactly, and descriptors may carry either spelling.

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
    copyMode: allResources
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    imageReference: >-
      ${target.baseUrl
      + (target.subPath == "" ? "" : "/" + target.subPath)
      + "/" + resource.access.toOCI().repository
      + (resource.access.toOCI().tag == "" ? "" : ":" + resource.access.toOCI().tag)}
```

Remove `uploadType` from the transfer config entry and add the OCI uploader
entry to the same config. The controller reads uploader entries from the same
configs (`LookupUploaderConfigs`).

{{< /tab >}}
{{< /tabs >}}

## Behaviour to know after migrating

- An uploader applies regardless of `--copy-resources`. An entry without a
  `match` therefore now also uploads OCI image and Helm resources without
  `--copy-resources` (they previously stayed by reference), which is why the
  local-blob-only row above exists.
- An uploader applies only if every identifier its `imageReference` uses is
  available. With the default template the resource keeps the default local-blob
  handling when:
  - the target is a CTF (no `target`);
  - `toOCI()` fails because the resource has no OCI reference (e.g. a local blob
    without `access.referenceName`).
- A local blob's `access.referenceName` is relative to the repository the blob is
  stored in, so `toOCI()` never reads a registry from it: every path component
  belongs to `repository`. `ghcr.io/org/image:v1` therefore still lands at
  `<target>/ghcr.io/org/image:v1`, as with the old flag.
- A local blob that is not an OCI manifest is never OCI-uploaded.
- Wget, S3 and GitHub resources are never OCI-uploaded.

The following table shows which access types the OCI uploader supports:

| Source access type | Applies when | `toOCI()` result |
| --- | --- | --- |
| `OCIImage` (all aliases) | always | Parsed from `imageReference`: `repository`, `tag`, `host`, `digest`, etc. E.g. `ghcr.io/org/image:v1` → repository `org/image`, tag `v1`. |
| `Helm` | always | Parsed from the chart reference: repository = repo URL path + chart name, tag = version. E.g. chart `podinfo:6.5.0` from `https://stefanprodan.github.io/podinfo` → repository `podinfo/podinfo`, tag `6.5.0`. |
| `LocalBlob` (OCI manifest media type) | media type is an OCI-compliant manifest | Parsed from `access.referenceName`, a repository name relative to the blob's repository: no registry, every path component belongs to `repository`. E.g. `stefanprodan/podinfo:6.5.0` → repository `stefanprodan/podinfo`, tag `6.5.0`. May have no tag or no reference. |
| anything else (Wget, S3, GitHub, …) | never | — (falls through) |

## Beyond the old flag

The OCI uploader configuration supports capabilities that `--upload-as` did not.

### Relocate images to a custom registry path

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
  imageReference: '${"ghcr.io/target-org/images/" + resource.access.toOCI().repository + ":" + resource.access.toOCI().tag}'
```

### Match a specific resource

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
  match:
    name: my-image
  imageReference: ghcr.io/target-org/special/my-image:1.0.0
```

### CTF target with a custom registry for artifacts

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
  imageReference: '${"registry.example.com/mirror/" + resource.access.toOCI().repository + ":" + resource.access.toOCI().tag}'
```

```bash
ocm transfer cv --copy-resources --config ./ocmconfig.yaml <src> ctf::./archive
```

### Build a reference from resource metadata

When the resource has no OCI reference name, build the reference from
`resource.name` and `resource.version` instead:

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
  imageReference: '${target.baseUrl + "/" + resource.name + ":" + resource.version}'
```

The two CEL identifiers available in `imageReference` templates are:

| Identifier | Value |
| --- | --- |
| `resource` | The source resource descriptor (same `resource` alias as the HTTP uploader). Call `resource.access.toOCI()` to get a map with `repository`, `tag`, `host`, `digest`, etc. |
| `target` | The OCI registry target: `target.baseUrl` (registry host) and `target.subPath` (repository prefix; may be `""`). Only available for OCI registry targets. |

## Verify the migration

Run the transfer with `--dry-run` to inspect the generated plan without writing
to the target:

```bash
ocm transfer cv --dry-run -o yaml --config ./ocmconfig.yaml <src> <target>
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
          ${{"baseUrl": "ghcr.io", "subPath": "target-org/ocm"}.baseUrl
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

### Symptom: `unknown flag: --upload-as`

**Cause:** The `--upload-as` flag has been removed.

**Fix:** Remove the flag from your command and add an OCI uploader entry to your
config as shown in the migration steps above.

### Symptom: `unknown field "uploadType"`

The full error is: `looking up transfer config failed: failed to decode transfer config: type "transfer.config.ocm.software/v1alpha1" has unknown or invalid fields: json: unknown field "uploadType"`.

**Cause:** The `uploadType` field has been removed from the transfer config type.
Strict decoding now rejects unknown fields.

**Fix:** Delete the `uploadType` field from your transfer config entry and add an
`oci.uploader.transfer.config.ocm.software/v1alpha1` uploader entry instead.

### Symptom: Resources still end up as local blobs

**Cause:** The uploader does not apply to the resource, so it falls through to
the default local blob handling. This happens when:

- the `imageReference` template uses `target` (the default does) and the target
  is not an OCI registry, for example a CTF archive;
- the template calls `toOCI()` (the default does) and the resource has no OCI
  reference, for example a local blob without `access.referenceName`;
- the resource is a local blob whose media type is not an OCI manifest.

Run with `--loglevel debug` to see the reason: the transfer logs
`oci uploader does not apply to resource` with a `reason` attribute.

**Fix:** For a CTF target, use an absolute registry prefix instead of `target`:

```yaml
imageReference: '${"ghcr.io/my-org/mirror/" + resource.access.toOCI().repository + ":" + resource.access.toOCI().tag}'
```

For a resource without a reference name, build the reference from the resource
instead:

```yaml
imageReference: '${target.baseUrl + "/" + resource.name + ":" + resource.version}'
```

## Related documentation

- [Transfer Configuration Reference]({{< relref "docs/reference/transfer-configuration.md" >}}) — Full configuration schema and field descriptions
- [Configure Custom Uploads During Transfer]({{< relref "docs/tutorials/configure-custom-uploads.md" >}}) — Tutorial for the HTTP streaming uploader
- [Transfer Helm Charts with OCM]({{< relref "docs/how-to/transfer-helm-charts.md" >}}) — Transfer component versions containing Helm charts
- [Replicate Component Versions with the Controller]({{< relref "docs/how-to/replicate-component-versions-controller.md" >}}) — Controller-based replication
