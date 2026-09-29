---
title: "Upload Helm Charts to Sonatype Nexus"
slug: "helm-charts"
description: "Upload Helm chart resources into a Sonatype Nexus helm hosted repository during transfer and publish them as Helm/v1 access."
weight: 1
toc: true
---

## Goal

Transfer a component version and upload its Helm chart into a Nexus helm hosted
repository, so that `helm pull` finds it.

## You'll end up with

- The chart stored in the Nexus helm repository `helm-hosted` and listed in its `index.yaml`
- A transferred resource with a `Helm/v1` access on that repository

**Estimated time:** ~10 minutes

## Prerequisites

- The setup in [Upload Resources to Sonatype Nexus]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/_index.md#prerequisites" >}}),
  with [credentials configured]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/_index.md#configure-credentials" >}})
- A Nexus helm hosted repository, here `helm-hosted`
- The `helm` CLI

## Steps

### Configure the uploader

Create `config.yaml` with the credentials and one uploader rule for the `chart` resource:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: Wget
          hostname: nexus.example.com
          scheme: https
        credentials:
          - type: WgetCredentials/v1
            username: <USERNAME>
            password: <PASSWORD_OR_USER_TOKEN>
  - type: nexus.uploader.transfer.config.ocm.software/v1alpha1
    match:
      accessType: localBlob
      name: chart
    url: https://nexus.example.com
    repository: helm-hosted
```

### Run the transfer

```bash
ocm transfer cv --config config.yaml ctf::./src//ocm.software/demo:1.0.0 ctf::./target
```

### Verify

```bash
ocm get cv ctf::./target//ocm.software/demo:1.0.0 -o yaml
```

The `chart` resource has this access:

```yaml
access:
  type: Helm/v1
  helmRepository: https://nexus.example.com/repository/helm-hosted
  helmChart: mychart:0.1.0
```

Pull the chart with Helm:

```bash
helm pull mychart --version 0.1.0 --repo https://nexus.example.com/repository/helm-hosted
```

## How the repository behaves

- Nexus stores the chart as `<name>-<version>.tgz` from its `Chart.yaml` and adds it
  to `index.yaml`.
- `path` is not supported: Nexus decides where charts are stored.
- A second transfer reuses the stored chart, whether the repository allows redeploys
  or not.
- Different content under a chart name and version that is already stored fails when
  the repository disallows redeploys.

## Troubleshooting

### Symptom: `content of resource … is not a helm chart`

**Cause:** The resource matched by a Helm repository uploader holds no packaged chart.

**Fix:** Narrow `match` to the chart resources, or route other resources to a raw
repository.

For credential and overwrite errors, see
[Troubleshooting]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/_index.md#troubleshooting" >}}).

## Related documentation

- [How-to: Upload Resources to Sonatype Nexus]({{< relref "docs/how-to/vendor-specific-apis/sonatype-nexus/_index.md" >}})
- [How-to: Upload Helm Charts to JFrog Artifactory]({{< relref "docs/how-to/vendor-specific-apis/jfrog-artifactory/helm-charts.md" >}})
- [Reference: Sonatype Nexus Uploader]({{< relref "docs/reference/transfer-configuration/nexus-uploader.md#repository-types" >}})
