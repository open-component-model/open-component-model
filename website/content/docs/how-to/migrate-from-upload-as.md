---
title: "Migrate from --upload-as to Uploader Configurations"
slug: "migrate-from-upload-as"
description: "Replace the removed --upload-as flag and uploadType transfer setting with the oci.uploader.transfer.config.ocm.software uploader configuration."
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
| `--copy-resources --upload-as ociArtifact`, or `copyMode: allResources` + `uploadType: ociArtifact` | Keep `--copy-resources` / `copyMode: allResources`; add `- type: oci.uploader.transfer.config.ocm.software/v1alpha1` (no other fields). | None. Same target references: `<target>/<referenceName>`. |
| `--upload-as ociArtifact` without `--copy-resources` (only OCI-manifest local blobs became OCI artifacts; OCI image / Helm references stayed by reference) | Two OCI uploader entries, `match: {accessType: LocalBlob}` and `match: {accessType: localBlob}`. | None. Both entries are needed because `match.accessType` compares the type name exactly, and descriptors carry either spelling. |
| Controller: `uploadType: ociArtifact` in the transfer config referenced by a `Replication` | Remove `uploadType` from that config entry; add the OCI uploader entry to the same config (ConfigMap/Secret). | None. The controller reads uploader entries from the same configs (`LookupUploaderConfigs`). |

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
```

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
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: localBlob
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
```

Remove `uploadType` from the transfer config entry and add the OCI uploader
entry to the same config. The controller reads uploader entries from the same
configs (`LookupUploaderConfigs`).

{{< /tab >}}
{{< /tabs >}}

## Behaviour to know after migrating

- An uploader applies regardless of `--copy-resources`. The plain entry without
  `--copy-resources` therefore now also uploads OCI image and Helm resources
  (which previously stayed by reference), which is why the local-blob-only row
  above exists.
- The same entry keeps local blobs when:
  - the target is a CTF;
  - a local blob has no `referenceName`;
  - a local blob is not an OCI manifest.
- Wget, S3 and GitHub resources are never OCI-uploaded.

The following table shows which access types the OCI uploader supports:

| Source access type | Applies when | `referenceName` |
| -------------------- | -------------- | ----------------- |
| `OCIImage` (all aliases) | always | `repository[:tag]` (registry and digest dropped from `imageReference`) |
| `Helm` | always | chart repository URL path, chart name and version (e.g. `podinfo/podinfo:6.5.0` for repository `https://stefanprodan.github.io/podinfo` and chart `podinfo:6.5.0`) |
| `LocalBlob` (OCI manifest media type) | media type is an OCI-compliant manifest | `access.referenceName` verbatim (may be empty) |
| anything else (Wget, S3, GitHub, …) | never | — (falls through) |

## Beyond the old flag

The OCI uploader configuration supports capabilities that `--upload-as` did not.

### Relocate images to a custom registry path

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
  imageReference: '${"ghcr.io/target-org/images/" + referenceName}'
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
  imageReference: '${"registry.example.com/mirror/" + referenceName}'
```

```bash
ocm transfer cv --copy-resources --config ./ocmconfig.yaml <src> ctf::./archive
```

The three CEL aliases available in `imageReference` templates are:

| Alias | Value |
| ------- | ------- |
| `resource` | The source resource descriptor (same as the HTTP uploader's `resource` alias). |
| `referenceName` | The derived reference name for the resource (only available when non-empty). |
| `targetRepository` | The target repository path (only available for OCI registry targets). |

## Verify the migration

Run the transfer with `--dry-run` to inspect the generated plan without writing
to the target:

```bash
ocm transfer cv --dry-run -o yaml --config ./ocmconfig.yaml <src> <target>
```

Check for `TransferOCIArtifact` / `AddOCIArtifact` nodes whose `imageReference`
starts with the expected registry path.

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
        imageReference: ghcr.io/target-org/ocm/stefanprodan/podinfo:6.5.0
        type: ociArtifact/v1
  type: TransferOCIArtifact/v1alpha1
```

For a Helm chart resource, you will see `GetHelmChart`, `ConvertHelmToOCI`, and
then `AddOCIArtifact/v1alpha1` whose `spec.resource.access.imageReference` is the
target reference, for example `ghcr.io/target-org/ocm/podinfo/podinfo:6.5.0`.

With `imageReference: '${"ghcr.io/mirror/" + referenceName}'` and a CTF target,
the dry-run shows the unevaluated CEL expression:

```yaml
imageReference: ${"ghcr.io/mirror/" + "stefanprodan/podinfo:6.5.0"}
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

**Cause:** The target is a CTF, the local blob has no `referenceName`, or its
media type is not an OCI manifest. Without `imageReference`, the OCI uploader
falls through to the default local blob handling in these cases.

**Fix:** Set `imageReference` to an absolute registry reference (e.g.
`'${"registry.example.com/mirror/" + referenceName}'`), or accept the local blob.

### Symptom: `imageReference references targetRepository, but target … is not an OCI registry`

**Cause:** The `imageReference` template uses the `targetRepository` alias, but
the transfer target is not an OCI registry (e.g. it is a CTF archive).

**Fix:** Use an absolute registry prefix instead of `targetRepository`:

```yaml
imageReference: '${"ghcr.io/my-org/mirror/" + referenceName}'
```

### Symptom: `imageReference references referenceName, but resource … has no reference name`

**Cause:** The matched resource does not have a reference name (e.g. a local blob
without `referenceName`).

**Fix:** Use `resource.name`, `resource.version`, or a literal value instead:

```yaml
imageReference: '${"ghcr.io/my-org/" + resource.name + ":" + resource.version}'
```

## Related documentation

- [Transfer Configuration Reference]({{< relref "docs/reference/transfer-configuration.md" >}}) — Full configuration schema and field descriptions
- [Configure Custom Uploads During Transfer]({{< relref "docs/tutorials/configure-custom-uploads.md" >}}) — Tutorial for the HTTP streaming uploader
- [Transfer Helm Charts with OCM]({{< relref "docs/how-to/transfer-helm-charts.md" >}}) — Transfer component versions containing Helm charts
- [Replicate Component Versions with the Controller]({{< relref "docs/how-to/replicate-component-versions-controller.md" >}}) — Controller-based replication
