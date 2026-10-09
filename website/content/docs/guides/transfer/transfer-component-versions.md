---
title: "Transfer Component Versions"
description: "Move a component version from one OCM repository to another with the default ocm transfer cv workflow, including credentials and verification."
weight: 10
toc: true
---

## Goal

Move a component version from a source OCM repository to a target OCM repository
with the default `ocm transfer cv` workflow. This is the base procedure that every
other transfer guide builds on.

## Prerequisites

- [OCM CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) installed
- A source OCM repository that holds the component version you want to move. This
  can be an OCI registry or a filesystem-based CTF archive.
- A target OCM repository you can write to, with credentials that grant write
  access. See [Configure Registry Credentials]({{< relref "docs/guides/transfer/configure-registry-credentials.md" >}})
  if you need to authenticate against multiple registries.

## How transfer works

`ocm transfer cv` reads a component version from the source and writes it to the
target. Any OCM repository can be a source or target: an OCI registry or a CTF
archive. A CTF (Common Transport Format) archive is a filesystem-based OCM
repository you can move with any file transfer mechanism, which makes it the
transport medium for offline and air-gapped scenarios.

By default, transfer copies **local blobs** (resources already embedded in the
source descriptor) into the target, while all other resources stay **by
reference**: the descriptor keeps pointing at their original access coordinates
(for example the OCI registry that holds a container image). The target then
depends on those locations still being reachable.

To produce a **self-contained** copy, where the referenced artifacts are pulled
from the source and re-uploaded into the target, add the `--copy-resources` flag.
This is essential whenever the target environment cannot reach the original
artifact locations.

## Steps

{{< steps >}}
{{< step >}}

### Transfer the component version

Point the command at your source and target repositories. Both arguments are OCM
repository references, so either side can be an OCI registry or a CTF archive:

```bash
ocm transfer cv \
  ghcr.io/<source-namespace>//ocm.software/examples/demo:1.0.0 \
  ghcr.io/<target-namespace>
```

By default this copies local blobs and keeps all other resources by reference, for
a single component version. To follow component references and transfer the full
graph, add `--recursive`:

```bash
ocm transfer cv --recursive \
  ghcr.io/<source-namespace>//ocm.software/examples/demo:1.0.0 \
  ghcr.io/<target-namespace>
```

{{< /step >}}
{{< step >}}

### Produce a self-contained copy (optional)

If the target must hold every artifact itself, add `--copy-resources`. The CLI
downloads each resource from the source and copies it into the target, so the
component version no longer depends on the original locations:

```bash
ocm transfer cv --copy-resources \
  ghcr.io/<source-namespace>//ocm.software/examples/demo:1.0.0 \
  ghcr.io/<target-namespace>
```

{{< /step >}}
{{< step >}}

### Verify the result

Confirm the component version is present in the target and, if it was signed,
that its signature still verifies. Signatures travel inside the component
descriptor, so they survive the transfer intact:

```bash
ocm get cv ghcr.io/<target-namespace>//ocm.software/examples/demo:1.0.0

ocm verify cv ghcr.io/<target-namespace>//ocm.software/examples/demo:1.0.0 \
  --public-key my-key.pub
```

{{< /step >}}
{{< /steps >}}

## Next steps

You transferred a component version with the default settings. Depending on where
and how the resources must land, continue with a specialized guide:

- [Upload OCI Images]({{< relref "docs/guides/transfer/upload-oci-images.md" >}})
  makes the component's container images directly pullable with `docker`, `oras`,
  or `crane`.
- [Transfer Components across an Air Gap]({{< relref "docs/guides/transfer/air-gap-transfer.md" >}})
  moves a self-contained copy across a network boundary.
- [Transfer Components with Helm Charts]({{< relref "docs/guides/transfer/transfer-helm-charts.md" >}})
  handles components that carry a Helm chart resource.
- [Upload to JFrog Artifactory]({{< relref "docs/guides/transfer/upload-to-jfrog-artifactory.md" >}})
  or [Upload to Sonatype Nexus]({{< relref "docs/guides/transfer/upload-to-sonatype-nexus.md" >}})
  route resources into a vendor registry's native layout.

For the model behind all of these, see
[Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}}).
