---
title: "Upload OCI Images"
description: "Configure the OCI uploader so a transferred component's images and Helm charts land as separate OCI artifacts you can pull with docker, oras, or crane."
weight: 115
toc: true
---

{{< callout context="note" title="Start from the base transfer guide" >}}
This guide builds on
[Transfer Component Versions]({{< relref "docs/guides/transfer/transfer-component-versions.md" >}}).
Read it first for the default `ocm transfer cv` workflow, credentials, and
verification; the steps below only add the OCI uploader configuration.
{{< /callout >}}

## Goal

Transfer a component version so that its container images (and Helm charts) land
in the target registry as **separate OCI artifacts**, directly addressable and
pullable with standard OCI tooling such as `docker`, `oras`, or `crane`, instead
of staying referenced in place or embedded as local blobs.

## How it works

Adding an `oci.uploader.transfer.config.ocm.software/v1alpha1` entry to your OCM
configuration tells transfer to upload every matched resource as its own OCI
artifact in the target registry. The resource is stored independently of the
component version, so it is directly pullable.

With no `match` field, the uploader uses its default selection: on OCI registry
targets it uploads `OCIImage` resources (all aliases), `Helm` chart resources, and
`LocalBlob` resources that hold an OCI manifest and carry a `referenceName`. That
is exactly the scope of the deprecated `--upload-as ociArtifact` flag. With no
`imageReference` field, the uploader derives the target reference automatically
from each resource's access type.

## Steps

{{< steps >}}
{{< step >}}

### Add the OCI uploader to your OCM configuration

Add a catch-all uploader entry to your `.ocmconfig` (or any OCM configuration you
pass with `--config`). Omitting `match` and `imageReference` uses the defaults,
which cover OCI images and Helm charts:

```yaml
# .ocmconfig
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
```

{{< /step >}}
{{< step >}}

### Transfer with the uploader applied

Run the base transfer with the configuration in effect:

```bash
ocm transfer cv --config .ocmconfig \
  ghcr.io/<source-namespace>//ocm.software/examples/demo:1.0.0 \
  ghcr.io/<target-namespace>
```

Each matched image or chart is pushed to the target registry under a reference
derived from its access type. For an OCI image such as `ghcr.io/org/image:v1`, the
uploader keeps the repository and tag (`org/image:v1`) and places it under the
target prefix.

{{< /step >}}
{{< step >}}

### Pull the uploaded artifact

The uploaded resources are now plain OCI artifacts in the target registry. Pull
one directly with any OCI client:

```bash
docker pull ghcr.io/<target-namespace>/org/image:v1
```

{{< /step >}}
{{< /steps >}}

## Targeting specific resources

To upload only selected resources, or to control the exact target reference, set
the `match` and `imageReference` fields. `match` is a CEL boolean over `resource`,
`component`, and `target`; `imageReference` is a CEL template (`${…}`) or a literal.
For example, to build the reference from resource metadata:

```yaml
- type: oci.uploader.transfer.config.ocm.software/v1alpha1
  imageReference: '${target.baseUrl + (target.subPath == "" ? "" : "/" + target.subPath) + "/" + resource.name + ":" + resource.version}'
```

An explicit `match` may select only resources the OCI uploader can upload: OCI
images, Helm charts, and local blobs holding an OCI manifest. Selecting anything
else fails the transfer.

For the full `match` / `imageReference` schema, the default expressions, and more
selection examples, see
[OCI Uploader]({{< relref "docs/reference/transfer-configuration/oci-uploader.md" >}}).

## Next steps

- To consume images that were uploaded this way, see
  [Pull OCI Artifacts Natively]({{< relref "docs/guides/transfer/pull-oci-artifacts-natively.md" >}}).
- If you are replacing the deprecated `--upload-as` flag, see
  [Migrate --upload-as Flags]({{< relref "docs/guides/transfer/migrate-from-upload-as.md" >}}).
