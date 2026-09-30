---
title: "Selection Examples"
description: "Executed selection examples for uploader configurations: which resources the OCI, local blob and reference uploaders select, and the resulting errors."
weight: 10
toc: true
---

The following examples are executed as tests (`TestUploaderExamples` in
`bindings/go/transfer/internal`), so each outcome below is what the transfer
does. They all transfer the component `ocm.software/demo:1.0.0` and, unless
stated otherwise, target the OCI registry `ghcr.io/target-org/ocm`
(`baseUrl: ghcr.io/target-org/ocm`, `subPath: ""`). The component has these
resources:

| Name     | Access                                                                                                       |
| -------- | ------------------------------------------------------------------------------------------------------------ |
| `app`    | `ociArtifact/v1` `imageReference: ghcr.io/acme/app:1.0.0`; label `{name: ocm.software/transfer, value: oci}` |
| `nginx`  | `ociArtifact/v1` `imageReference: docker.io/library/nginx:1.25`                                              |
| `chart`  | `helm/v1` `helmRepository: https://charts.acme.io/stable`, `helmChart: app`, `version: 1.0.0`                |
| `bundle` | `LocalBlob/v1` `mediaType: application/vnd.oci.image.manifest.v1+json`, `referenceName: acme/bundle:1.0.0`   |
| `notes`  | `LocalBlob/v1` `mediaType: text/plain`                                                                       |
| `docs`   | `Wget/v1` `url: https://docs.acme.io/guide.tar`                                                              |

Outcomes:

- **oci `<ref>`**: pushed as a separate OCI artifact to `<ref>`.
- **local blob**: embedded in the target as a local blob.
- **by reference**: not copied; the access is unchanged in the target.

The baseline applies to resources no uploader selects: local blobs are copied
as local blobs and all other resources stay by reference. A catch-all
`localblob.uploader…` entry copies every supported resource.

## E1 — default OCI uploader (replaces --upload-as ociArtifact)

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
```

| Resource | Outcome                                                                  |
| -------- | ------------------------------------------------------------------------ |
| `app`    | oci `ghcr.io/target-org/ocm/acme/app:1.0.0`                              |
| `nginx`  | oci `ghcr.io/target-org/ocm/library/nginx:1.25`                          |
| `chart`  | oci `ghcr.io/target-org/ocm/stable/app:1.0.0`                            |
| `bundle` | oci `ghcr.io/target-org/ocm/acme/bundle:1.0.0`                           |
| `notes`  | local blob (the default `match` does not select it: not an OCI manifest) |
| `docs`   | by reference (the default `match` does not select `Wget`)                |

## E2 — the same with every default spelled out

Writing the default `match` and the default
[`imageReference`]({{< relref "docs/reference/transfer-configuration/oci-uploader.md#imagereference" >}})
into the entry gives the same outcome as E1. There is no reason to do so except
as a starting point for a change.

## E3 — Helm charts only

An explicit `match` replaces the default entirely, so it must repeat the target check.

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: target.type == "OCIRepository" && resource.access.isType("Helm")
```

| Resource               | Outcome                                       |
| ---------------------- | --------------------------------------------- |
| `chart`                | oci `ghcr.io/target-org/ocm/stable/app:1.0.0` |
| `app`, `nginx`, `docs` | by reference                                  |
| `bundle`, `notes`      | local blob                                    |

## E4 — OCI-manifest local blobs only

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

| Resource                        | Outcome                                        |
| ------------------------------- | ---------------------------------------------- |
| `bundle`                        | oci `ghcr.io/target-org/ocm/acme/bundle:1.0.0` |
| `notes`                         | local blob                                     |
| `app`, `nginx`, `chart`, `docs` | by reference                                   |

## E5 — only images from Docker Hub

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: >-
      target.type == "OCIRepository"
      && resource.access.isType("OCIImage")
      && (resource.access.toOCI().host == "docker.io"
        || resource.access.toOCI().host.endsWith(".docker.io"))
```

| Resource               | Outcome                                         |
| ---------------------- | ----------------------------------------------- |
| `nginx`                | oci `ghcr.io/target-org/ocm/library/nginx:1.25` |
| `app`, `chart`, `docs` | by reference                                    |
| `bundle`, `notes`      | local blob                                      |

`toOCI()` normalizes Docker Hub references to the host `registry-1.docker.io`,
hence `endsWith`. `toOCI()` is only called for `OCIImage` accesses: `&&`
short-circuits, so the Helm and local blob resources never reach it.

## E6 — select by label

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: >-
      target.type == "OCIRepository"
      && resource.access.isType("OCIImage")
      && has(resource.labels)
      && resource.labels.exists(l, l.name == "ocm.software/transfer" && l.value == "oci")
```

| Resource                 | Outcome                                     |
| ------------------------ | ------------------------------------------- |
| `app`                    | oci `ghcr.io/target-org/ocm/acme/app:1.0.0` |
| `nginx`, `chart`, `docs` | by reference                                |
| `bundle`, `notes`        | local blob                                  |

`has(resource.labels)` is required because `labels` is omitted from a resource
that has none.

## E7 — CTF target, images mirrored to a registry

Target `ctf::./archive`. The template does not use `target`, so it evaluates
for a CTF target.

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("OCIImage")
    imageReference: '${"registry.example.com/mirror/" + resource.access.toOCI().repository + ":" + resource.access.toOCI().tag}'
```

| Resource          | Outcome                                              |
| ----------------- | ---------------------------------------------------- |
| `app`             | oci `registry.example.com/mirror/acme/app:1.0.0`     |
| `nginx`           | oci `registry.example.com/mirror/library/nginx:1.25` |
| `chart`, `docs`   | by reference                                         |
| `bundle`, `notes` | local blob                                           |

## E8 — relocate one resource, default for the rest

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.name == "app"
    imageReference: ghcr.io/target-org/special/app:1.0.0
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
```

| Resource                                    | Outcome                                                                                                                                             |
| ------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| `app`                                       | oci `ghcr.io/target-org/special/app:1.0.0` (first entry: `match` selects by name, default `match` of second entry would also select but first wins) |
| `nginx`, `chart`, `bundle`, `notes`, `docs` | as in E1 (second entry)                                                                                                                             |

## E9 — errors instead of silent skipping

Each case transfers a component with only the named resource.

| Config entry                                                               | Resource / target              | Error contains                                                                                                     |
| -------------------------------------------------------------------------- | ------------------------------ | ------------------------------------------------------------------------------------------------------------------ |
| OCI, `match: resource.access.isType("Wget")`                               | `docs`                         | `oci uploader cannot upload access type Wget/v1`                                                                   |
| OCI, `match: resource.access.isType("LocalBlob")`                          | `notes`                        | `not an OCI manifest`                                                                                              |
| OCI, `match: resource.access.isType(`                                      | `app`                          | `invalid match`                                                                                                    |
| OCI, `match: '"yes"'`                                                      | `app`                          | `must evaluate to a bool`                                                                                          |
| OCI, `match: resource.access.isType("OCIImage")`, default `imageReference` | `app`, target `ctf::./archive` | `imageReference does not evaluate` (the default template reads `target.baseUrl`, which a CTF target does not have) |

## E10 — copy everything as local blobs (replaces --copy-resources)

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
```

| Resource | Outcome    |
| -------- | ---------- |
| `app`    | local blob |
| `nginx`  | local blob |
| `chart`  | local blob |
| `bundle` | local blob |
| `notes`  | local blob |
| `docs`   | local blob |

`chart` goes through GetHelmChart → ConvertHelmToOCI → OCIAddLocalResource;
`docs` through DownloadWgetResource → OCIAddLocalResource.
A config with only a `localblob.uploader…` entry (no other uploaders) produces the same graph.

## E11 — OCI artifacts plus everything else copied

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
```

| Resource | Outcome                                         |
| -------- | ----------------------------------------------- |
| `app`    | oci `ghcr.io/target-org/ocm/acme/app:1.0.0`     |
| `nginx`  | oci `ghcr.io/target-org/ocm/library/nginx:1.25` |
| `chart`  | oci `ghcr.io/target-org/ocm/stable/app:1.0.0`   |
| `bundle` | oci `ghcr.io/target-org/ocm/acme/bundle:1.0.0`  |
| `notes`  | local blob                                      |
| `docs`   | local blob                                      |

## E12 — exclude one resource from a catch-all

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: reference.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.name == "nginx"
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
```

| Resource | Outcome                                        |
| -------- | ---------------------------------------------- |
| `nginx`  | by reference                                   |
| `app`    | oci `ghcr.io/target-org/ocm/acme/app:1.0.0`    |
| `chart`  | oci `ghcr.io/target-org/ocm/stable/app:1.0.0`  |
| `bundle` | oci `ghcr.io/target-org/ocm/acme/bundle:1.0.0` |
| `notes`  | local blob                                     |
| `docs`   | local blob                                     |

## E13 — keep Docker Hub images by reference, copy the rest

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: reference.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("OCIImage") && (resource.access.toOCI().host == "docker.io" || resource.access.toOCI().host.endsWith(".docker.io"))
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
```

| Resource | Outcome      |
| -------- | ------------ |
| `nginx`  | by reference |
| `app`    | local blob   |
| `chart`  | local blob   |
| `bundle` | local blob   |
| `notes`  | local blob   |
| `docs`   | local blob   |

## E14 — copy only wget downloads

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("Wget")
```

| Resource                | Outcome               |
| ----------------------- | --------------------- |
| `docs`                  | local blob            |
| `app`, `nginx`, `chart` | by reference          |
| `bundle`, `notes`       | local blob (baseline) |

## E15 — errors

Each case transfers a component with only the named resource.

| Config entry               | Resource                                     | Error contains                                          |
| -------------------------- | -------------------------------------------- | ------------------------------------------------------- |
| reference, `match: "true"` | `bundle`                                     | `local blobs cannot be kept by reference`               |
| localblob, `match: "true"` | `custom` with access `{"type": "Custom/v1"}` | `local blob uploader cannot copy access type Custom/v1` |

The [Complete Example]({{< relref "docs/reference/transfer-configuration/_index.md#complete-example" >}}) on the overview page is executed as E16.
