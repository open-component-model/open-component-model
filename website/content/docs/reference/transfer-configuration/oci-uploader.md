---
title: "OCI Uploader"
description: "Reference for oci.uploader.transfer.config.ocm.software/v1alpha1: upload matched resources as separate OCI artifacts."
weight: 2
toc: true
---

Uploads a selected resource as a separate OCI artifact in the target registry.
The resource is stored independently from the component version, making it
directly addressable and pullable with standard OCI tools.

## Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/OCIUploaderConfig.schema.json" >}}

## Fields

| Field            | Type           | Required | Description                                                                                                                                                                                                                                           |
| ---------------- | -------------- | -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `match`          | CEL expression | no       | A CEL boolean expression selecting the resources this uploader handles (`resource`, `component` and `target` are available; test access types with `resource.access.isType`). When omitted, the default below applies; an explicit value replaces it. |
| `imageReference` | CEL expression | no       | Target image reference: a `${…}` CEL template or a plain literal. Defaults to the expression described in [`imageReference`](#imagereference).                                                                                                        |

## Default `match`

```yaml
match: >-
  target.type == "OCIRepository"
  && (resource.access.isType(["OCIImage", "Helm"])
    || (resource.access.isType("LocalBlob")
      && has(resource.access.mediaType) && isOCIManifest(resource.access.mediaType)
      && has(resource.access.referenceName)))
```

The OCI uploader selects, on OCI registry targets only:

| Source access type                  | Selected when                                                          | Name used in the default `imageReference`                                                                                                                   |
| ----------------------------------- | ---------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `OCIImage` (all aliases)            | always                                                                 | `resource.access.toOCI().repository` + tag. E.g. `ghcr.io/org/image:v1` → `org/image:v1` (registry and digest dropped).                                     |
| `Helm`                              | always                                                                 | Helm repository URL path + chart name, tagged with version. E.g. `https://stefanprodan.github.io/podinfo`, chart `podinfo:6.5.0` → `podinfo/podinfo:6.5.0`. |
| `LocalBlob`                         | the media type is an OCI manifest and the access has a `referenceName` | `resource.access.referenceName` verbatim (whatever it contains, including host/port/digest). E.g. `ghcr.io/org/image:v1` → `ghcr.io/org/image:v1`.          |
| anything else (Wget, S3, GitHub, …) | never                                                                  | —                                                                                                                                                           |

This is exactly the scope of the deprecated `--upload-as ociArtifact`. An explicit
`match` may select only resources the OCI uploader can upload: OCI images, Helm
charts, and local blobs holding an OCI manifest. Selecting anything else fails
the transfer with `oci uploader cannot upload access type …` or `… not an OCI
manifest`.

## `imageReference`

`imageReference` is a CEL template (`${…}`) or a plain literal. It sees the same
identifiers as `match`:

| Identifier  | Value                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| ----------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `resource`  | The source resource descriptor (same `resource` alias as the HTTP uploader). Fields are resolved dynamically (`dyn()`), so a template may use `has()` and read fields of any access type. For OCI image accesses, call `resource.access.toOCI()` to obtain a map with keys `host`, `registry` (= host), `repository`, `tag`, `digest`, `reference`. This is the same `toOCI()` function the OCM Kubernetes controller offers in its CEL expressions. In transfers, `toOCI()` resolves OCI image accesses only. |
| `component` | The source component version: `component.name`, `component.version`, `component.provider`.                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `target`    | The transfer target. An OCI registry exposes `target.baseUrl` (the registry, including a scheme if the target has one, e.g. `http://127.0.0.1:5000`) and `target.subPath` (the repository prefix; may be `""`). A CTF archive exposes `target.filePath`.                                                                                                                                                                                                                                                       |

When `imageReference` is omitted, the uploader uses the default for the access
type of the selected resource. Each produces `<baseUrl>[/<subPath>]/<name>` and
starts with the same target prefix,
`target.baseUrl + (target.subPath == "" ? "" : "/" + target.subPath) + "/"`:

- **OCI image**: the repository and tag of the image reference, via `toOCI()`
  (registry and digest dropped). E.g. `ghcr.io/org/image:v1` → `<target>/org/image:v1`.

  ```yaml
  imageReference: |-
    ${target.baseUrl + (target.subPath == "" ? "" : "/" + target.subPath) + "/"
      + resource.access.toOCI().repository
      + (resource.access.toOCI().tag == "" ? "" : ":" + resource.access.toOCI().tag)}
  ```

- **Helm chart**: the path of `helmRepository` (empty segments dropped) plus the
  chart name, tagged with `version` or the part after `:` in `helmChart`. E.g.
  `https://stefanprodan.github.io/podinfo` + `podinfo:6.5.0` → `<target>/podinfo/podinfo:6.5.0`.

  ```yaml
  imageReference: |-
    ${target.baseUrl + (target.subPath == "" ? "" : "/" + target.subPath) + "/"
      + (url(resource.access.helmRepository).path.split("/") + [resource.access.helmChart.split(":")[0]]).filter(s, s != "").join("/")
      + (has(resource.access.version) && resource.access.version != ""
        ? ":" + resource.access.version
        : (resource.access.helmChart.contains(":") ? ":" + resource.access.helmChart.split(":")[1] : ""))}
  ```

- **Local blob** holding an OCI manifest: `access.referenceName` verbatim
  (whatever it contains, including host/port/digest). E.g. `ghcr.io/org/image:v1`
  → `<target>/ghcr.io/org/image:v1`.

  ```yaml
  imageReference: '${target.baseUrl + (target.subPath == "" ? "" : "/" + target.subPath) + "/" + resource.access.referenceName}'
  ```

These are the same references as the old `--upload-as ociArtifact`. Writing the
default for an access type into an entry that selects only that access type is
equivalent to omitting it. An explicit `imageReference` applies to every
resource the entry selects, so an entry that selects several access types needs
a template that works for all of them, or one entry per access type (see E2 in
the [selection examples]({{< relref "docs/reference/transfer-configuration/selection-examples.md" >}})).

The template is evaluated for every selected resource while the graph is built.
A template that does not evaluate for a selected resource fails the transfer
with `imageReference does not evaluate`, for example a default reference on a
CTF target (it reads `target.baseUrl`), or `resource.access.toOCI()` on a Helm
chart. A template that does not use `target` (for example an absolute registry
prefix) also works for CTF targets, because `TransferOCIArtifact` and
`AddOCIArtifact` push to the templated image reference independently of the
component target; its `match` must then select CTF targets (see E7 in the
[selection examples]({{< relref "docs/reference/transfer-configuration/selection-examples.md" >}})).

## More examples

Relocate local blobs under a mirror using `referenceName` directly. The
default `match` also selects OCI images and Helm charts, which have no
`referenceName`, so restrict the uploader to local blobs:

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
  match: resource.access.isType("LocalBlob") && has(resource.access.mediaType) && isOCIManifest(resource.access.mediaType) && has(resource.access.referenceName)
  imageReference: '${"ghcr.io/mirror/" + resource.access.referenceName}'
```

Build a reference from resource metadata (works for any resource the uploader can upload):

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
  imageReference: '${target.baseUrl + (target.subPath == "" ? "" : "/" + target.subPath) + "/" + resource.name + ":" + resource.version}'
```

Upload a local blob to its `referenceName` as-is, e.g. when the name already is a
full reference such as `ghcr.io/org/image:v1`:

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
  match: resource.name == "my-image"
  imageReference: '${resource.access.referenceName}'
```

See [Migrate from --upload-as to Uploader Configurations]({{< relref "docs/how-to/migration-guides/migrate-from-upload-as.md" >}}).
