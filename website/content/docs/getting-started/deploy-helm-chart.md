---
title: "Deploy Helm Charts"
description: "Deploy a Helm chart from an OCM component version with OCM Controllers, kro, and Flux."
icon: "🚀"
weight: 26
toc: true
---

This tutorial walks you through deploying a Helm chart from an OCM component version to a Kubernetes cluster,
using the OCM Controllers with kro and Flux.

## What You'll Learn

- Create and publish an OCM component version that references a Helm chart
- Define a ResourceGraphDefinition to orchestrate OCM and Flux resources
- Deploy the Helm chart to your cluster using the OCM Controllers

## Estimated time

~20 minutes

## Scenario

You as a developer create an application, packaged as a Helm chart, and publish it as OCM component version in an OCI registry.
Then, you as an operator deploy the Helm chart into a Kubernetes cluster using the OCM Controllers.
You define a ResourceGraphDefinition that tells kro how to orchestrate the OCM and Flux resources to deploy the Helm chart.

## Prerequisites

- [Controller environment]({{< relref "setup-controller-environment.md" >}}) set up (OCM Controllers, kro and Flux in a Kubernetes cluster)
- [Custom RBAC]({{< relref "custom-rbac.md" >}}) configured to allow the controller to manage `ResourceGraphDefinitions`
- [OCM CLI]({{< relref "ocm-cli-installation.md" >}}) installed
- Access to an OCI registry (e.g., [ghcr.io](https://docs.github.com/en/packages/learn-github-packages/introduction-to-github-packages))
- `envsubst` installed (pre-installed on most Linux/macOS systems; part of `gettext`)

## Environment Setup

Set environment variables for your GitHub username and OCM repository:

```bash
export GITHUB_USERNAME=<your-github-username>
export OCM_REPO=ghcr.io/$GITHUB_USERNAME/ocm-tutorial
```

## Create and Publish a Component Version

{{< steps >}}
{{< step >}}

### Create a working directory

```shell
mkdir /tmp/helm-deploy && cd /tmp/helm-deploy
```
{{< /step >}}

{{< step >}}

### Define the component

Create a `component-constructor.yaml` file that includes a Helm chart resource:

```yaml
components:
  - name: ocm.software/ocm-k8s-toolkit/simple
    provider:
      name: ocm.software
    version: "1.0.0"
    resources:
      - name: helm-resource
        type: helmChart
        version: 1.0.0
        access:
          type: ociArtifact
          imageReference: "ghcr.io/stefanprodan/charts/podinfo:6.11.1@sha256:a9b2804ec61795a7457b2303bf9efbc5fba51f856c3945f3bb0af68bf3b35afd"
```

This component references the `podinfo` Helm chart, a simple web application that displays pod information.
{{< /step >}}

{{< step >}}

### Build the component version

Now add the component version to a local Common Transport Format (CTF) archive.
By default, the `ocm add cv` command looks for a file called `component-constructor.yaml` in the current directory,
and creates a CTF archive in a folder called `transport-archive`:

```shell
ocm add cv
```

<details>
<summary>You should see this output</summary>

```text
time=2026-02-25T12:47:09.622+01:00 level=WARN msg="could not get credential consumer identity for component version repository" repository=transport-archive error="failed to get component version repository: cannot resolve consumer identity for ctf: credentials not supported"
COMPONENT                           │ VERSION │ PROVIDER
────────────────────────────────────┼─────────┼──────────────
ocm.software/ocm-k8s-toolkit/simple │ 1.0.0   │ ocm.software
```
</details>
{{< /step >}}

{{< step >}}

### Transfer to your registry

Use `ocm transfer cv` and specify the correct reference (`<path-to-your-ctf>//<component>:<version>`) and target repository:

{{< callout title="Note" icon="outline/info-circle" >}}
If your registry requires authentication, configure [Credentials for OCM CLI]({{< relref "/docs/how-to/configure-multiple-credentials.md" >}}) first.
{{< /callout >}}

```shell
ocm transfer cv transport-archive//ocm.software/ocm-k8s-toolkit/simple:1.0.0 $OCM_REPO
```

<details>
<summary>You should see this output</summary>

```text
Transferring component versions...
  ✓ transformOcmSoftwareOcmK8sToolkitSimple100Upload [OCIAddComponentVersion]
  [████████████████████████████████████████] 100% 1/1
```
</details>

To make your component public in GitHub Container Registry, go to the `packages` tab in your GitHub repository `https://github.com/$GITHUB_USERNAME?tab=packages`,
select the package `component-descriptors/ocm.software/ocm-k8s-toolkit/simple`, and under "Package settings" change the visibility to `public`.

Alternatively, if you want to keep your package private, configure credentials for the OCM Controllers:

{{< details "Configure credentials for private registries" >}}
Create a docker-registry secret with your registry credentials. For GitHub Container Registry, you can use a Personal Access Token or a short-lived token from the GitHub CLI:

```shell
kubectl create secret docker-registry ghcr-secret \
  --docker-username=$GITHUB_USERNAME \
  --docker-password="$(gh auth token)" \
  --docker-server=ghcr.io
```

Then update the resources to use credentials:

1. **OCM Controller resources**: Add `ocmConfig` to the Repository resource in your RGD. The credentials propagate automatically to Component, Resource, and Deployer objects that reference this Repository:

  ```yaml
      - id: repository
        readyWhen:
          - ${repository.status.conditions.exists(c, c.type == 'Ready' && c.status == 'True')}
        template:
          apiVersion: delivery.ocm.software/v1alpha1
          kind: Repository
          metadata:
            name: simple-repository
          spec:
            repositorySpec:
                baseUrl: $OCM_REPO
                type: OCIRegistry
            interval: 1m
            ocmConfig:
              - kind: Secret
                name: ghcr-secret
  ```

For more details, see [Credentials for OCM Controllers]({{< relref "/docs/how-to/configure-credentials-ocm-controllers.md" >}}).
{{< /details >}}

{{< /step >}}

{{< step >}}

### Verify the upload

```shell
ocm get cv $OCM_REPO//ocm.software/ocm-k8s-toolkit/simple:1.0.0
```

<details>
<summary>You should see this output</summary>

```text
 COMPONENT                           │ VERSION │ PROVIDER
─────────────────────────────────────┼─────────┼──────────────
 ocm.software/ocm-k8s-toolkit/simple │ 1.0.0   │ ocm.software
```
</details>

{{< /step >}}
{{< /steps >}}

## Deploy Helm chart using the OCM Controllers

{{< steps >}}
{{< step >}}

### Create ResourceGraphDefinition

The ResourceGraphDefinition tells kro how to orchestrate the OCM and Flux resources.
Create `rgd.yaml` with the following content:

{{< details "ResourceGraphDefinition (rgd.yaml)" >}}
```yaml
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: simple
spec:
  schema:
    apiVersion: v1alpha1
    # The name of the CRD that is created by this ResourceGraphDefinition when applied
    kind: Simple
    spec:
      # This spec defines values that can be referenced in the ResourceGraphDefinition and that can be set in the
      # instances of this ResourceGraphDefinition.
      # We will use it to pass a value to the Helm chart and configure the message the application shows
      # (see resource HelmRelease).
      message: string | default="foo"
  resources:
    # Repository points to the OCM repository in which the OCM component version is stored and checks if it is
    # reachable by pinging it.
    - id: repository
      readyWhen:
        - ${repository.status.conditions.exists(c, c.type == 'Ready' && c.status == 'True')}
      template:
        apiVersion: delivery.ocm.software/v1alpha1
        kind: Repository
        metadata:
          name: simple-repository
        spec:
          repositorySpec:
              baseUrl: $OCM_REPO
              type: OCIRegistry
          interval: 1m
          # ocmConfig is required, if the OCM repository requires credentials to access it.
          # ocmConfig:
    # Component refers to the Repository, downloads and verifies the OCM component version descriptor.
    - id: component
      readyWhen:
        - ${component.status.conditions.exists(c, c.type == 'Ready' && c.status == 'True')}
      template:
        apiVersion: delivery.ocm.software/v1alpha1
        kind: Component
        metadata:
          name: simple-component
        spec:
          repositoryRef:
            name: ${repository.metadata.name}
          component: ocm.software/ocm-k8s-toolkit/simple
          semver: 1.0.0
          interval: 1m
          # ocmConfig is required, if the OCM repository requires credentials to access it.
          # ocmConfig:
    # Resource points to the Component, downloads the resource passed by reference-name and verifies it. It then
    # publishes the location of the resource in its status.
    - id: resourceChart
      readyWhen:
        - ${resourceChart.status.conditions.exists(c, c.type == 'Ready' && c.status == 'True')}
      template:
        apiVersion: delivery.ocm.software/v1alpha1
        kind: Resource
        metadata:
          name: simple-resource
        spec:
          componentRef:
            name: ${component.metadata.name}
          resource:
            byReference:
              resource:
                name: helm-resource # This must match the resource name set in the OCM component version (see above)
          additionalStatusFields:
            # toOCI() converts the resource access to an OCI reference object containing registry, repository, tag, and digest
            oci: resource.access.toOCI()
          # ocmConfig is required, if the OCM repository requires credentials to access it.
          # ocmConfig:
    # OCIRepository watches and downloads the resource from the location provided by the Resource status.
    # The Helm chart location (url) refers to the status of the above resource.
    - id: ocirepository
      readyWhen:
        - ${ocirepository.status.conditions.exists(c, c.type == 'Ready' && c.status == 'True')}
      template:
        apiVersion: source.toolkit.fluxcd.io/v1
        kind: OCIRepository
        metadata:
          name: simple-ocirepository
        spec:
          interval: 1m0s
          layerSelector:
            mediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip"
            operation: copy
          url: oci://${resourceChart.status.additional.oci.registry}/${resourceChart.status.additional.oci.repository}
          ref:
            tag: ${resourceChart.status.additional.oci.tag}
          # secretRef is required, if the OCI repository requires credentials to access it.
          # secretRef:
    # HelmRelease refers to the OCIRepository, lets you configure the Helm chart and deploys the Helm chart into the
    # Kubernetes cluster.
    - id: helmrelease
      readyWhen:
        - ${helmrelease.status.conditions.exists(c, c.type == 'Ready' && c.status == 'True')}
      template:
        apiVersion: helm.toolkit.fluxcd.io/v2
        kind: HelmRelease
        metadata:
          name: simple-helmrelease
        spec:
          releaseName: simple
          interval: 1m
          timeout: 5m
          chartRef:
            kind: OCIRepository
            name: ${ocirepository.metadata.name}
            namespace: default
          values:
            # We configure the Helm chart using Flux's HelmRelease 'values' field. We pass the value that we set in
            # the instance of the CRD created by the ResourceGraphDefinition (see below).
            ui:
              message: ${schema.spec.message}
```
{{< /details >}}
{{< /step >}}

{{< step >}}

### Apply the ResourceGraphDefinition

```shell
envsubst < rgd.yaml | kubectl apply -f -
```

<details>
<summary>You should see this output</summary>

```text
resourcegraphdefinition.kro.run/simple created
```
</details>

Verify it's active:

```shell
kubectl get rgd
```

<details>
<summary>You should see this output</summary>

```text
NAME     APIVERSION   KIND     STATE    AGE
simple   v1alpha1     Simple   Active   19s
```
</details>
<br>

A new Custom Resource Definition called `Simple` that you can now instantiate has been created.
{{< /step >}}

{{< step >}}

### Create an instance

Create the file `instance.yaml` to deploy the application:

```yaml
apiVersion: kro.run/v1alpha1
kind: Simple
metadata:
  name: simple
spec:
  message: "Deployed with OCM!"
```
{{< /step >}}

{{< step >}}
**Deploy the application**

```shell
kubectl apply -f instance.yaml
```

<details>
<summary>You should see this output</summary>

```text
simple.kro.run/simple created
```
</details>

Wait for the deployment to complete:

```shell
kubectl get simple -w
```

<details>
<summary>You should see this output</summary>

```text
NAME     STATE    SYNCED   AGE
simple   ACTIVE   True     2m
```
</details>
{{< /step >}}

{{< step >}}

### Verify the deployment

Check that the pod is running:

```shell
kubectl get deployments
```

Output:

```text
NAME             READY   UP-TO-DATE   AVAILABLE   AGE
simple-podinfo   1/1     1            1           3m
```

Verify the custom message you configured in the HelmRelease values is showing up in the application:

```shell
kubectl get pods -l app.kubernetes.io/name=simple-podinfo \
  -o jsonpath='{.items[0].spec.containers[0].env[?(@.name=="PODINFO_UI_MESSAGE")].value}'
```

Output:

```text
Deployed with OCM!
```
{{< /step >}}
{{< /steps >}}

## Troubleshooting

### Authentication Errors

If you see errors like:

```text
failed to list versions: response status code 401: unauthorized
```

Your registry package is private. Either:

- Make the package public in your registry settings, or
- [Configure credentials]({{< relref "/docs/how-to/configure-credentials-ocm-controllers.md" >}}) as
  described in the collapsible section after "Transfer to your registry"

### Resource Not Found

If the component isn't found, verify:

- The component was transferred successfully: `ocm get cv $OCM_REPO//ocm.software/ocm-k8s-toolkit/simple:1.0.0`
- The `baseUrl` in the ResourceGraphDefinition matches your registry

### RBAC Permission Errors

If the controller logs show permission errors like `forbidden` or `cannot create resource`, the controller lacks RBAC permissions to manage `ResourceGraphDefinitions`. Follow the [Custom RBAC guide]({{< relref "custom-rbac.md" >}}) to grant the necessary permissions.

## Cleanup

Remove the deployed resources:

```shell
kubectl delete -f instance.yaml
kubectl delete -f rgd.yaml
```

Remove the temporary files:

```shell
rm -rf /tmp/helm-deploy
```

## Next Steps

- [Tutorial: Create a Multi-Component Product]({{< relref "docs/tutorials/advanced-component-constructor.md" >}}) - Learn how to structure complex applications with multiple components and resources
- [Tutorial: Deploy a Helm Chart (with Bootstrap)]({{< relref "docs/tutorials/deploy-helm-chart-bootstrap.md" >}}) - Use the OCM Controllers to deploy a Helm chart without manual bootstrapping, using GitOps

## Related Documentation

- [Concept: OCM Controllers]({{< relref "ocm-controllers.md" >}}) - Learn about the architecture and capabilities of the OCM Controllers
