---
title: "Transfer Helm Charts with OCM"
description: "Transfer component versions containing Helm chart resources between OCI registries using the OCM CLI."
weight: 12
toc: true
---

## Goal

Transfer a component version that contains a Helm chart resource sourced from a **Helm repository** (type `helmChart` with `helm/v1` access)
to an OCI registry. This guide does not cover charts that are already stored as OCI artifacts.

## You'll end up with

- A component version containing a Helm chart transferred to a target OCI registry

**Estimated time:** ~10 minutes

## Prerequisites

- [OCM CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) installed
- Write access to a target OCI registry with [credentials configured]({{< relref "docs/how-to/configure-multiple-credentials.md" >}})
- A component version containing a Helm chart resource (stored in a CTF archive or OCI registry)

## Steps

{{< steps >}}

{{<callout context="note" title="Helm charts stored as ociImage" icon="outline/info-circle">}}
If your existing component version contains a Helm chart stored as an `ociImage` resource rather than a `helmChart` with `helm/v1` access, you need to
create a new component version using the `helmChart` type as shown below. This guide only covers the transfer of Helm charts sourced from Helm
repositories.
{{< /callout >}}

{{< step >}}

### Create a component version with a Helm chart resource

If you already have a component version containing a Helm chart with `helm/v1` access, skip to the next step.

Create a `constructor.yaml` that references a chart from a Helm repository:

```yaml
components:
  - name: example.com/my-app
    version: 1.0.0
    provider:
      name: example.com
    resources:
      - name: my-chart
        version: 1.0.0
        type: helmChart
        access:
          type: helm/v1
          helmRepository: https://charts.example.com
          helmChart: my-chart-1.0.0.tgz
```

Add the component version to a CTF archive:

```bash
ocm add cv --repository ctf::<path/to/archive> \
  --constructor constructor.yaml \
  --skip-reference-digest-processing
```

{{< /step >}}

{{<callout context="caution" title="Why --skip-reference-digest-processing?" icon="outline/alert-triangle">}}
The `--skip-reference-digest-processing` flag is required because the `helm/v1` access type currently cannot be fully resolved during `add cv`.
The chart is not downloaded at this stage, so digest calculation is not possible. The digests are computed later during transfer when the chart is
actually fetched.
Full Helm support is being tracked as a future feature in [ocm-project#911](https://github.com/open-component-model/ocm-project/issues/911).
{{< /callout >}}

{{< step >}}

### Transfer the component version

Transfer the component version to the target registry. Use `--copy-resources` to include the Helm chart and `--upload-as ociArtifact` to store it as a
standalone OCI artifact in the target registry.

{{< callout title="Note" icon="outline/info-circle" >}}
The `--upload-as` flag is a temporary solution. It will be superseded by the upcoming transfer specification.
See [ocm-project#925](https://github.com/open-component-model/ocm-project/issues/925) for details.
{{< /callout >}}

```bash
ocm transfer cv \
  --copy-resources \
  --upload-as ociArtifact \
  ctf::<path/to/archive>//<component-name>:<version> \
  <target-registry>
```

During transfer, the Helm chart is always converted to an OCI artifact. With `--upload-as ociArtifact`,
this artifact is uploaded as a separate image in the target registry.
The component descriptor references it via an `imageReference` (e.g., `ghcr.io/my-org/charts/my-chart:1.0.0`),
making it independently addressable and pullable with `helm pull`. For more details on how transfers and resource handling work,
see [Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}}).

Alternatively, `--upload-as localBlob` embeds the chart directly in the component version's blob store.
This keeps the chart coupled to the component version but means it is not independently addressable in the registry and cannot be pulled with the Helm
CLI.

To find the `imageReference`, inspect the component descriptor:

```bash
ocm get cv <target-registry>//<component-name>:<version> -o yaml
```

In the output, look for the `resources[].access.imageReference` field:

```yaml
resources:
  - name: my-chart
    type: helmChart
    access:
      type: ociArtifact/v1
      imageReference: ghcr.io/my-org/charts/my-chart:1.0.0
```

Use the `imageReference` value with Helm's OCI support:

```bash
helm pull oci://ghcr.io/my-org/charts/my-chart --version 1.0.0
```

{{< /step >}}
{{< step >}}

### Verify the transfer

Confirm the component version is available in the target registry:

```bash
ocm get cv <target-registry>//<component-name>:<version>
```

Download the chart resource to verify it was transferred correctly:

```bash
ocm download resource \
  <target-registry>//<component-name>:<version> \
  --identity name=my-chart \
  --output ./downloaded
```

{{< /step >}}
{{< /steps >}}

## Transfer between registries

To transfer a Helm chart component version from one OCI registry to another, use the source registry reference directly:

```bash
ocm transfer cv \
  --copy-resources \
  --upload-as ociArtifact \
  <source-registry>//<component-name>:<version> \
  <target-registry>
```

## Tips

- **If provenance files (`.prov`) are present** in the Helm repository, they are automatically included in the transfer.
- **If you need to transfer recursively**, add `--recursive` to include all transitively referenced component versions.
- **For air-gapped environments**, first transfer to a CTF archive, move it across the boundary, then import into the target registry.
  See [Transfer Components Across an Air Gap]({{< relref "docs/how-to/air-gap-transfer.md" >}}).

## Next Steps

- [How to: Transfer Components Across an Air Gap]({{< relref "docs/how-to/air-gap-transfer.md" >}}) - Transfer components between disconnected networks
- [How-To: Download Resources from Component Versions]({{< relref "docs/how-to/download-resources-from-component-versions.md" >}}) - Download individual resources from a component version

## Related documentation

- [Concept: Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}}) - Understand how OCM moves component versions between repositories
