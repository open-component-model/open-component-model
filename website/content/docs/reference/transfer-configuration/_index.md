---
title: "Transfer Configuration"
description: "Reference for OCM transfer configuration: transfer settings, uploader configurations, resource matching, CEL expressions, and selection semantics."
icon: "🚚"
weight: 7
toc: true
sidebar:
  collapsed: true
---

This page is the technical reference for OCM transfer configuration. For a
task-oriented walkthrough of routing resources to a custom upload target, see the
[Configure Custom Uploads During Transfer]({{< relref "docs/tutorials/configure-custom-uploads.md" >}})
tutorial. For the conceptual model, see
[Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}}).

## Configuration Types

Transfer behaviour is controlled by configuration types embedded in the
standard OCM configuration file. They are carried as entries inside the central
`generic.config.ocm.software/v1` configuration and may appear together:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    recursive: -1
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("Wget/v1")
    targetURL: '${"https://mytarget.registry.com/uploads" + url(resource.access.url).path}'
    method: PUT
```

| Type                                                         | Purpose                                                                     | Reference                                                                                                    |
| ------------------------------------------------------------ | --------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `transfer.config.ocm.software/v1alpha1`                      | Global transfer settings: recursion depth.                                  | [Transfer Settings]({{< relref "docs/reference/transfer-configuration/transfer-settings.md" >}})             |
| `oci.uploader.transfer.config.ocm.software/v1alpha1`         | Per-match rule that uploads a resource as a separate OCI artifact.          | [OCI Uploader]({{< relref "docs/reference/transfer-configuration/oci-uploader.md" >}})                       |
| `http.uploader.transfer.config.ocm.software/v1alpha1`        | Per-match rule that streams a resource to a custom HTTP target.             | [HTTP Uploader]({{< relref "docs/reference/transfer-configuration/http-uploader.md" >}})                     |
| `localblob.uploader.transfer.config.ocm.software/v1alpha1`   | Per-match rule that downloads a resource and embeds it as a local blob.     | [Local Blob Uploader]({{< relref "docs/reference/transfer-configuration/localblob-uploader.md" >}})          |
| `reference.uploader.transfer.config.ocm.software/v1alpha1`   | Per-match rule that keeps a resource by reference (no transformation).      | [Reference Uploader]({{< relref "docs/reference/transfer-configuration/reference-uploader.md" >}})           |
| `artifactory.uploader.transfer.config.ocm.software/v1alpha1` | Per-match rule that uploads a resource into a JFrog Artifactory repository. | [JFrog Artifactory Uploader]({{< relref "docs/reference/transfer-configuration/artifactory-uploader.md" >}}) |
| `nexus.uploader.transfer.config.ocm.software/v1alpha1`       | Per-match rule that uploads a resource into a Sonatype Nexus repository.    | [Sonatype Nexus Uploader]({{< relref "docs/reference/transfer-configuration/nexus-uploader.md" >}})          |

By default the CLI looks for configuration in `$HOME/.ocmconfig`. Pass
`--config <file>` to use a different file. `--recursive` overrides the transfer
config when set.

### Deprecated Flags

The `--copy-resources` and `--upload-as` flags are deprecated. They are
translated into uploader configuration entries appended after all configured
entries. See the
[migration guide]({{< relref "docs/how-to/migrate-from-upload-as.md" >}}) for
details.

## Uploader Configurations

An **uploader configuration** routes the resources it selects to a custom
target instead of the default handling. Six config types are available:

| Uploader                | What it does                                       | Default `match`                                                                                                | `match` required? |
| ----------------------- | -------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | ----------------- |
| `oci.uploader…`         | Uploads a resource as a separate OCI artifact      | Yes (see [OCI Uploader]({{< relref "docs/reference/transfer-configuration/oci-uploader.md#default-match" >}})) | No                |
| `localblob.uploader…`   | Downloads a resource and embeds it as a local blob | `resource.access.isType(["LocalBlob", "OCIImage", "OCIImageLayer", "Helm", "Wget", "S3/v2", "GitHub"])`        | No                |
| `reference.uploader…`   | Keeps a resource by reference (no transformation)  | `!resource.access.isType("LocalBlob")`                                                                         | No                |
| `http.uploader…`        | Streams a resource to an HTTP endpoint             | None                                                                                                           | **Yes**           |
| `artifactory.uploader…` | Uploads into a JFrog Artifactory repository        | None                                                                                                           | **Yes**           |
| `nexus.uploader…`       | Uploads into a Sonatype Nexus repository           | None                                                                                                           | **Yes**           |

Each entry is an independent rule; you may declare several.

### Selection

For each resource, uploaders are evaluated in declaration order. An uploader
**selects** a resource when its `match` evaluates to `true`. An omitted `match`
means the uploader type's default match; the HTTP, Artifactory, and Nexus
uploaders have no default and require `match`.

The **first** uploader that selects a resource handles it. There is no
fall-through: if the selected uploader cannot handle the resource (for example
an OCI uploader selecting a `Wget` resource, or an `imageReference` that does
not evaluate for the resource), the transfer fails with an error that names the
uploader and the resource.

A resource that no uploader selects follows the baseline: local blobs are
copied as local blobs; all other resources stay by reference (their access is
unchanged in the target). A catch-all
`localblob.uploader.transfer.config.ocm.software/v1alpha1` entry copies
every supported resource. Declare more specific rules before broader ones.

A warning is logged for an uploader whose match selected no resource.

{{< callout context="note" >}}
The OCM Kubernetes controller accepts transfer config and
OCI, local blob and reference uploader entries. HTTP, Artifactory, and Nexus
uploader entries are ignored by the controller because they send content to
configured URLs from the controller pod.
{{< /callout >}}

### `match`

`match` is a plain CEL boolean expression (not wrapped in `${…}`). It is
evaluated once per resource while the transfer graph is built. The following
identifiers are available:

| Identifier  | Value                                                                                                                                                                          |
| ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `resource`  | The source resource, dynamically typed, so `has(resource.access.<field>)` and field reads compile for every access type.                                                       |
| `component` | The source component version: `component.name`, `component.version`, `component.provider`.                                                                                     |
| `target`    | The transfer target as a map. An OCI registry is `{"type": "OCIRepository", "baseUrl": …, "subPath": …}`; a CTF archive is `{"type": "CommonTransportFormat", "filePath": …}`. |

Available functions:

| Function                               | Description                                                                                                                                                               |
| -------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `resource.access.isType(string)`       | True when the access type matches the argument, with alias and version resolution (see below).                                                                            |
| `resource.access.isType(list(string))` | True when the access type matches **any** element in the list.                                                                                                            |
| `isOCIManifest(string)`                | True for OCI image manifest and index, and Docker manifest and manifest list media types.                                                                                 |
| `toOCI()`                              | Returns a map with keys `host`, `registry`, `repository`, `tag`, `digest`, `reference` for OCI image accesses.                                                            |
| `url(string)`                          | Parses a URL and returns a map with keys `scheme`, `host`, `hostname`, `port`, `path`, `rawPath`, `rawQuery`, `fragment`, `user`.                                         |
| String extensions                      | `split`, `join`, `endsWith`, `startsWith`, `contains`, `replace`, `trim`, … from the [CEL string extensions](https://github.com/google/cel-go/blob/master/ext/README.md). |

#### `isType` semantics

`resource.access.isType(arg)` receives the resource's access map and tests
its `type` field against the argument. Both sides are resolved through the
transfer access scheme (alias resolution):

- **Unversioned argument**: matches any version. `isType("OCIImage")` matches
  `ociArtifact/v1`, `ociImage/v1`, `ociRegistry/v1`, etc.
- **Versioned argument**: the resolved types must be equal, so aliases of the
  same version match (`isType("ociImage/v1")` matches `ociArtifact/v1`), but
  `isType("S3/v1")` does not match `s3/v2`.
- **Unregistered types** (for example `Custom/v1`) resolve to themselves:
  `isType("Custom")` matches `Custom/v1` by name.

| Access `type` in descriptor | `isType("OCIImage")` | `isType("ociArtifact/v1")` | `isType("Helm")` | `isType("LocalBlob")` |
| --------------------------- | -------------------- | -------------------------- | ---------------- | --------------------- |
| `ociArtifact/v1`            | yes                  | yes                        | no               | no                    |
| `ociImage/v1`               | yes                  | yes                        | no               | no                    |
| `ociRegistry/v1`            | yes                  | yes                        | no               | no                    |
| `localBlob/v1`              | no                   | no                         | no               | yes                   |
| `helm/v1`                   | no                   | no                         | yes              | no                    |
| `Wget/v1`                   | no                   | no                         | no               | no                    |

- Every uploader type except `http`, `artifactory`, and `nexus` has a default
  `match` (see the type sections). An explicit `match` **replaces** the default
  entirely; writing the default out is equivalent to omitting it.
- A `match` that does not compile, does not evaluate, or does not return a bool
  fails the transfer.

### Complete Example

The following shows a full config migrated from the old `--copy-resources` plus
`--upload-as ociArtifact` style.

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    recursive: -1
  # 1. Keep the large base image by reference (not copied at all).
  #    Declared first, so the catch-all below never sees it.
  - type: reference.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.name == "base-os-image"
  # 2. Stream wget-hosted documentation to an HTTP artifact store.
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("Wget/v1")
    targetURL: '${"https://artifacts.example.com/ocm" + url(resource.access.url).path}'
    method: PUT
  # 3. OCI images, Helm charts and OCI-manifest local blobs become separate OCI
  #    artifacts next to the component version (former --upload-as ociArtifact).
  #    Default match and imageReference.
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
  # 4. Everything the rules above did not select is embedded as a local blob
  #    (former --copy-resources). Default match.
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
```

Entries 3 and 4 need no fields: their default `match` (see the
[OCI Uploader]({{< relref "docs/reference/transfer-configuration/oci-uploader.md#default-match" >}}) and
[Local Blob Uploader]({{< relref "docs/reference/transfer-configuration/localblob-uploader.md#default-match" >}})
sections) and the default `imageReference` are what the former settings did.

Outcome for `ocm.software/demo:1.0.0` transferred to `ghcr.io/target-org/ocm`:

| Resource        | Access                                                  | Selected by                                   | Result in target                                             |
| --------------- | ------------------------------------------------------- | --------------------------------------------- | ------------------------------------------------------------ |
| `base-os-image` | `OCIImage/v1` `ghcr.io/acme/base-os:1.2`                | 1                                             | unchanged `ghcr.io/acme/base-os:1.2`                         |
| `docs`          | `Wget/v1` `https://docs.example.com/demo/guide.tar`     | 2                                             | `Wget/v1` `https://artifacts.example.com/ocm/demo/guide.tar` |
| `app-image`     | `OCIImage/v1` `ghcr.io/acme/app:1.0.0`                  | 3                                             | `ociArtifact/v1` `ghcr.io/target-org/ocm/acme/app:1.0.0`     |
| `chart`         | `helm/v1` `https://charts.acme.io/stable` + `app:1.0.0` | 3                                             | `ociArtifact/v1` `ghcr.io/target-org/ocm/stable/app:1.0.0`   |
| `config`        | `LocalBlob/v1` `application/json`                       | 4 (3's `match` is false: not an OCI manifest) | `LocalBlob/v1`                                               |
| `sources`       | `GitHub/v1` (pinned commit)                             | 4                                             | `LocalBlob/v1`                                               |
| `models`        | `S3/v2`                                                 | 4                                             | `LocalBlob/v1`                                               |

Transferring the same config to a CTF target: entry 3's `match` is false for every
resource, so `app-image` and `chart` are copied as local blobs by entry 4.

## Notes

### Precedence

For a given resource, uploaders are evaluated in declaration order and the first
whose `match` selects the resource handles it. A
selected uploader that cannot handle the resource fails the transfer. A resource
no uploader selects follows the baseline: local blobs are copied as local blobs,
all other resources stay by reference. See [Selection](#selection).

### Deterministic Plans

Transfer produces a deterministic transformation plan: components are processed in
sorted order and transformation identifiers are derived from stable hashes. The
plan is rendered with human-readable labels such as
`my-app@1.0.0 [Stream icons to mytarget.registry.com]`.

## Related Documentation

- [Configure Custom Uploads During Transfer]({{< relref "docs/tutorials/configure-custom-uploads.md" >}}) — tutorial that walks through an uploader end to end
- [Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}}) — the conceptual transfer model
- [Working with HTTP Resources]({{< relref "docs/tutorials/wget-http-resources.md" >}}) — the `Wget/v1` type produced by the HTTP streaming uploader
- [HTTP Client Configuration]({{< relref "docs/reference/http-client-configuration.md" >}}) — tuning the HTTP client used for the upload
- [Migrate from --upload-as to Uploader Configurations]({{< relref "docs/how-to/migrate-from-upload-as.md" >}}) — migration from deprecated flags
