---
title: "Upload Helm Charts to JFrog Artifactory"
slug: "helm-charts"
description: "Upload Helm chart resources into a JFrog Artifactory helm repository during transfer and publish them as Helm/v1 access."
weight: 1
toc: true
---

## Goal

Transfer a component version and upload its Helm chart into an Artifactory helm
repository, so that `helm pull` finds it.

## You'll end up with

- The chart stored in the Artifactory helm repository `helm-local`
- A transferred resource with a `Helm/v1` access on that repository

**Estimated time:** ~10 minutes

## Prerequisites

- The setup in [JFrog Artifactory]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/_index.md#prerequisites" >}}),
  with [credentials configured]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/_index.md#configure-credentials" >}})
- An Artifactory helm repository, here `helm-local`
- The `helm` CLI

## Steps

### Configure the uploader

Add the credentials and one uploader rule for the `chart` resource to your
`.ocmconfig` in the working directory (merged with `$HOME/.ocmconfig`):

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: Wget
          hostname: myorg.jfrog.io
          scheme: https
        credentials:
          - type: WgetCredentials/v1
            identityToken: <ARTIFACTORY_IDENTITY_TOKEN>
  - type: artifactory.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("LocalBlob") && resource.name == "chart"
    url: https://myorg.jfrog.io
    repository: helm-local
```

### Run the transfer

```bash
ocm transfer cv ctf::./src//ocm.software/demo:1.0.0 ctf::./target
```

### Verify

```bash
ocm get cv ctf::./target//ocm.software/demo:1.0.0 -o yaml
```

The `chart` resource has this access:

```yaml
access:
  type: Helm/v1
  helmRepository: https://myorg.jfrog.io/artifactory/api/helm/helm-local
  helmChart: mychart:0.1.0
```

Pull the chart with Helm:

```bash
helm pull mychart --version 0.1.0 --repo https://myorg.jfrog.io/artifactory/api/helm/helm-local
```

## How the repository behaves

- The chart is stored under `<component>/<component version>/<resource>-<resource version>.tgz`.
  A custom `path` must end in `.tgz`.
- The published chart name and version come from the chart itself, not from the resource.
- The repository must not enforce chart name and version in file names (*Helm
  Enforce Layout*): the file name comes from the OCM resource, not from the chart.
- A second transfer uploads nothing: the uploader logs
  `reused helm chart content already stored in the helm repository`.

## Troubleshooting

### Symptom: `content of resource … is not a helm chart`

**Cause:** The resource matched by a Helm repository uploader holds no packaged chart.
Artifactory deletes the uploaded file again.

**Fix:** Narrow `match` to the chart resources, or route other resources to a generic
repository.

For credential and overwrite errors, see
[Troubleshooting]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/_index.md#troubleshooting" >}}).

## Related documentation

- [How-to: JFrog Artifactory]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/_index.md" >}})
- [How-to: Upload Helm Charts to Sonatype Nexus]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/helm-charts.md" >}})
- [Reference: JFrog Artifactory Uploader]({{< relref "docs/reference/transfer-configuration/artifactory-uploader.md#repository-types" >}})
- [JFrog: Helm Chart Repositories](https://jfrog.com/help/r/jfrog-artifactory-documentation/helm-chart-repositories)
