---
title: "Add Helm Chart Resources"
description: "Add a Helm chart to a component version as a helmChart resource, either embedded with the Helm/v1 input or referenced with the Helm/v1 access, ready to transfer to an OCI registry."
weight: 35
toc: true
---

Package a Helm chart into an OCM component version as a `helmChart` resource. A
chart can be **referenced** in place so the component only records where to pull
it, or **embedded** by value so it travels with the component. When the chart is
stored in an **OCI registry**, reference it with the `OCIImage/v1` access. This is
the **recommended** way: the chart stays a native OCI artifact, directly
`helm pull oci://`-able, with no copy at build time.

## What You'll Learn

- Reference an OCI-hosted chart with the `OCIImage/v1` access (recommended)
- Reference a chart in an HTTP/HTTPS Helm repository with the `Helm/v1` access
- Embed a local or remote chart by value with the `Helm/v1` input
- Build the component version with `ocm add cv` and inspect the stored resource

## Scenario

You are packaging an application as an OCM component and the deployment is a Helm
chart. You want the chart to be a first-class resource of the component so it is
signed, transported, and versioned together with everything else. Modern charts
are published to OCI registries, so you reference them there. If a chart only
lives in a classic HTTP Helm repository, or must be self-contained, you fall back
to the other forms.

## How It Works

A `helmChart` resource carries the chart in one of three ways, in order of
preference:

1. **`access: OCIImage/v1` (recommended)** references a chart that is published as
   an OCI artifact in a registry. Nothing is copied at build time, the chart stays
   natively addressable, and it is pullable with `helm pull oci://`. Use this
   whenever the chart lives in an OCI registry.
2. **`access: Helm/v1`** references a chart in a classic HTTP/HTTPS Helm chart
   repository. Use it only when the chart is not available over OCI. This access
   does not support `oci://` repositories.
3. **`input: Helm/v1`** reads the chart at build time (from a local `path` or a
   remote `helmRepository`) and stores it **by value** as a local blob. Use it when
   the chart must travel self-contained inside the component.

Referenced charts are marked `relation: external`. On transfer, OCM resolves the
chart and, with the OCI uploader applied, uploads it as a standalone OCI artifact
in the target registry (see
[Upload OCI Images]({{< relref "docs/guides/transfer/upload-oci-images.md" >}})).

## Prerequisites

- [OCM CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) installed
- A Helm chart published to an OCI registry (recommended), an HTTP/HTTPS Helm repository, or available on the local filesystem

## Authoring

{{< tabs "helm-use-cases" >}}

{{< tab "Reference an OCI chart (recommended)" >}}

When the chart is stored in an OCI registry, reference it with the `OCIImage/v1`
access. The resource type stays `helmChart`, and the `imageReference` points at
the chart's OCI reference. Nothing is copied at build time.

{{< steps >}}
{{< step >}}

### Create the component constructor (OCI access)

```yaml
# yaml-language-server: $schema=https://ocm.software/{{< site-version >}}/schemas/bindings/go/constructor/schema-2020-12.json
components:
  - name: example.com/my-app
    version: 1.0.0
    provider:
      name: example.com
    resources:
      - name: podinfo-chart
        type: helmChart
        version: 6.9.1
        relation: external
        access:
          type: OCIImage/v1
          imageReference: ghcr.io/stefanprodan/charts/podinfo:6.9.1
```

`imageReference` is the full OCI reference of the chart (no `oci://` prefix). The
chart is already a native OCI artifact, so consumers can pull it directly:

```bash
helm pull oci://ghcr.io/stefanprodan/charts/podinfo --version 6.9.1
```

{{< /step >}}
{{< step >}}

### Build the component version (OCI access)

```bash
ocm add cv --repository ctf::./transport-archive \
  --constructor constructor.yaml
```

<details>
<summary>Expected output</summary>

```text
 COMPONENT            | VERSION | PROVIDER
----------------------+---------+-------------
 example.com/my-app   | 1.0.0   | example.com
```

</details>

The component version now references the chart as an OCI artifact, ready to
transfer.

{{< /step >}}
{{< /steps >}}

{{< /tab >}}

{{< tab "Reference an HTTP chart (access)" >}}

When the chart lives in a classic HTTP/HTTPS Helm chart repository and is not
available over OCI, reference it with the `Helm/v1` access. Nothing is copied at
build time, and the resource is marked `external`.

{{< steps >}}
{{< step >}}

### Create the component constructor (Helm access)

```yaml
# yaml-language-server: $schema=https://ocm.software/{{< site-version >}}/schemas/bindings/go/constructor/schema-2020-12.json
components:
  - name: example.com/my-app
    version: 1.0.0
    provider:
      name: example.com
    resources:
      - name: mariadb-chart
        type: helmChart
        version: 12.2.7
        relation: external
        access:
          type: Helm/v1
          helmChart: mariadb:12.2.7
          helmRepository: https://charts.bitnami.com/bitnami
```

`helmChart` is the chart name and version in the repository, `helmRepository` is
the HTTP(S) chart repository URL. The `Helm/v1` access only supports HTTP/HTTPS
repositories. For an OCI-hosted chart, use the recommended `OCIImage/v1` access
instead.

{{< /step >}}
{{< step >}}

### Build the component version (Helm access)

```bash
ocm add cv --repository ctf::./transport-archive \
  --constructor constructor.yaml
```

The component version now records a reference to the chart. The chart is resolved
on access rather than stored in the component.

{{< /step >}}
{{< /steps >}}

{{< /tab >}}

{{< tab "Embed a chart (input)" >}}

When the chart must travel self-contained, embed it by value with the `Helm/v1`
input. Supply exactly one of `path` (a local chart) or `helmRepository` (a remote
chart to fetch and embed).

{{< steps >}}
{{< step >}}

### Create the component constructor (embedded input)

```yaml
# yaml-language-server: $schema=https://ocm.software/{{< site-version >}}/schemas/bindings/go/constructor/schema-2020-12.json
components:
  - name: example.com/my-app
    version: 1.0.0
    provider:
      name: example.com
    resources:
      # Local chart directory
      - name: my-chart
        type: helmChart
        version: 1.0.0
        input:
          type: Helm/v1
          path: ./charts/my-app
          repository: charts/my-app:1.0.0
      # Remote chart fetched over HTTP and embedded
      - name: ingress-chart
        type: helmChart
        version: 4.14.0
        input:
          type: Helm/v1
          helmRepository: https://github.com/kubernetes/ingress-nginx/releases/download/helm-chart-4.14.0/ingress-nginx-4.14.0.tgz
      # Remote chart fetched over OCI and embedded
      - name: podinfo-chart
        type: helmChart
        version: 6.9.1
        input:
          type: Helm/v1
          helmRepository: oci://ghcr.io/stefanprodan/charts/podinfo:6.9.1
          repository: charts/podinfo:6.9.1
```

Supply exactly one of `path` or `helmRepository` per resource. For a local chart,
`path` points at the chart directory or packaged `*.tgz`. For a remote chart,
`helmRepository` is an HTTP(S) `*.tgz` URL or an `oci://` reference. The optional
`repository` sets the chart's name and tag inside the component.

{{< /step >}}
{{< step >}}

### Build the component version (embedded input)

```bash
ocm add cv --repository ctf::./transport-archive \
  --constructor constructor.yaml
```

The chart is now embedded as a local blob in a component version inside the CTF
archive, ready to transfer.

{{< /step >}}
{{< /steps >}}

{{< /tab >}}

{{< /tabs >}}

## Inspect the resource

Examine the component descriptor to confirm how the chart was stored:

```bash
ocm get cv ./transport-archive//example.com/my-app:1.0.0 -o yaml
```

An OCI-referenced chart keeps its `OCIImage/v1` access, an HTTP-referenced chart
keeps its `Helm/v1` access, and an embedded chart appears with a `localBlob`
access. Download an embedded chart to verify it was packaged correctly:

```bash
ocm download resource \
  ./transport-archive//example.com/my-app:1.0.0 \
  --identity name=my-chart \
  --output ./downloaded
```

## Next Steps

- [Upload OCI Images]({{< relref "docs/guides/transfer/upload-oci-images.md" >}}) — transfer the component version so the chart lands as a standalone, `helm pull`-able OCI artifact
- [Add OCI Artifacts]({{< relref "docs/guides/pack/add-oci-artifacts.md" >}}) — embed container images and OCI-hosted charts as native OCI artifacts

## Related Documentation

- [Reference: Input and Access Types]({{< relref "docs/reference/input-and-access-types.md" >}}#helmv1) — full field schema for the `Helm/v1` input and access
- [Concept: Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}}) — how OCM moves component versions and resources between repositories
