---
title: Deploying a Helm Chart
description: "Demonstrates how to deploy a Helm Chart from an OCM component version using OCM K8s Toolkit, kro, and FluxCD."
icon: ":motor_boat:"
weight:
toc: true
---

# Deploying a Helm Chart

This guide demonstrates how to deploy a Helm Chart from an OCM component version using OCM K8s Toolkit, kro, and FluxCD.
It is a rather basic example, in which it is assumed that a developer created an application, packaged it as a Helm
chart, and publishes it as OCM component version in an OCI registry. Then, an operator who wants to deploy the
application via Helm chart in a Kubernetes cluster, creates a `ResourceGraphDefinition` with resources that point to
this OCM component version. Using CEL expressions inside the `ResourceGraphDefinition`, the information about the
resource location will be passed to FluxCD, which will then configure the Helm chart and deploy it into the Kubernetes
cluster.

> [!IMPORTANT]
> Before starting, make sure you have set up your environment as described in the [setup guide](setup.md).

## Create the OCM component version

First, we will create an OCM component version containing a Helm chart. For this example, we will use the `podinfo`
Helm chart, which is a simple web application that serves a pod information page. For more details on how to create an
OCM component version, please refer to the [OCM documentation][ocm-doc].

To create the OCM component version, we will use the following `component-constructor.yaml` file:

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
          imageReference: "ghcr.io/stefanprodan/charts/podinfo:6.9.1@sha256:565d310746f1fa4be7f93ba7965bb393153a2d57a15cfe5befc909b790a73f8a"
```

After creating the file, we can create the OCM component version:

```bash
ocm add componentversion --create --file ./ctf component-constructor.yaml
```

This will create a local CTF (Component Transfer Format) directory `./ctf` containing the OCM component version. Since
the OCM component version must be accessible for the OCM K8s Toolkit controllers, we will transfer the CTF to a
registry. For this example, we will use GitHub's container registry, but you can use any OCI registry:

```bash
ocm transfer ctf ./ctf ghcr.io/<your-namespace>
```

> [!NOTE]
> If you are using a registry that requires authentication, you need to provide credentials for ocm. Please refer to
> the [OCM CLI credentials documentation][ocm-credentials] for more information on how to set up and use credentials.

If everything went well, you should see the following output:

```bash
ocm get componentversion ghcr.io/<your-namespace>//ocm.software/ocm-k8s-toolkit/simple:1.0.0
```

```console
COMPONENT                           VERSION PROVIDER
ocm.software/ocm-k8s-toolkit/simple 1.0.0   ocm.software
```

## Deploy the Helm Chart

To deploy the Helm chart from the OCM component version, we will first create a `ResourceGraphDefinition` that contains
all required resources. Additionally, we will add a configuration to the `HelmRelease` resource that can be passed
through the instance of that `ResourceGraphDefinition`. After the `ResourceGraphDefinition` is created and applied, we
create the instance of the `ResourceGraphDefinition` that will deploy the Helm chart.

### Create and apply the `ResourceGraphDefinition`

The `ResourceGraphDefinition` is a custom resource that defines all the resources that should be applied. To proceed
with the example, create a file named `rgd.yaml` and add the following content:

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
      template:
        apiVersion: delivery.ocm.software/v1alpha1
        kind: Repository
        metadata:
          name: simple-repository
        spec:
          repositorySpec:
              baseUrl: ghcr.io/<your-namespace>
              type: OCIRegistry
          interval: 1m
          # ocmConfig is required, if the OCM repository requires credentials to access it.
          # ocmConfig:
    # Component refers to the Repository, downloads and verifies the OCM component version descriptor.
    - id: component
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
            # The additional status fields are useful for splitting the imageReference into its components, so that
            # they can be used in depending deployers
            # Example: ghcr.io/stefanprodan/charts/podinfo:6.7.1 would be 
            # registry: ghcr.io
            # repository: stefanprodan/charts/podinfo
            # reference/tag: 6.7.1
            registry: resource.access.imageReference.toOCI().registry
            repository: resource.access.imageReference.toOCI().repository
            tag: resource.access.imageReference.toOCI().tag
          interval: 1m
          # ocmConfig is required, if the OCM repository requires credentials to access it.
          # ocmConfig:
    # OCIRepository watches and downloads the resource from the location provided by the Resource status.
    # The Helm chart location (url) refers to the status of the above resource.
    - id: ocirepository
      template:
        apiVersion: source.toolkit.fluxcd.io/v1beta2
        kind: OCIRepository
        metadata:
          name: simple-ocirepository
        spec:
          interval: 1m0s
          layerSelector:
            mediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip"
            operation: copy
          url: oci://${resourceChart.status.additional.registry}/${resourceChart.status.additional.repository}
          ref:
            tag: ${resourceChart.status.additional.tag}
          # secretRef is required, if the OCI repository requires credentials to access it.
          # secretRef:
    # HelmRelease refers to the OCIRepository, lets you configure the helm chart and deploys the Helm Chart into the
    # Kubernetes cluster.
    - id: helmrelease
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
            # We configure the Helm chart using FluxCDs HelmRelease 'values' field. We pass the value that we set in
            # the instance of the CRD created by the ResourceGraphDefinition (see below).
            ui:
              message: ${schema.spec.message}
```

> [!NOTE]
> If you pushed the OCM component version to a private registry, you need to set up the credentials for the OCM K8s
> Toolkit resources. You can do this by uncommenting the `ocmConfig` fields in the `Repository`, `Component`, and
> `Resource` resources and providing the necessary credentials. For more information on how to set up and pass the
> credentials, please check out the guide [configure credentials for OCM K8s Toolkit resources](credentials.md).
>
> Be aware that FluxCD's `OCIRepository` also needs access to the OCI registry that contains the Helm chart. However,
> `OCIRepository` only accepts
> [`imagePullSecrets`](https://fluxcd.io/flux/components/source/ocirepositories/#secret-reference) in the same namespace.
> If you want to use the same credentials for FluxCD and for the OCM K8s Toolkit resources, create a
> [Kubernetes secret of type `dockerconfigjson`](credentials.md#create-a-kubernetes-secret-of-type-dockerconfigjson-to-access-private-ocm-repositories)
> and keep all the resources in the same namespace.

After creating the file `rgd.yaml` with the above content and adjusting Repository's `baseUrl` to point to your OCM
repository, you can apply the `ResourceGraphDefinition` to your Kubernetes cluster:

```bash
kubectl apply -f rgd.yaml
```

If everything went well, you should see the following output:

```bash
kubectl get rgd
```

```console
NAME     APIVERSION   KIND     STATE    AGE
simple   v1alpha1     Simple   Active   19s
```

This creates a Kubernetes Custom Resource Definition (CRD) `Simple` that can be used to create instances. An applied
instance of the CRD will create all resources defined in the `ResourceGraphDefinition`.

### Create an Instance of "Simple"

To create an instance of the `Simple` CRD, create a file named `instance.yaml` and add the following content:

```yaml
apiVersion: kro.run/v1alpha1
# Kind is the CRD name that was created by the ResourceGraphDefinition
kind: Simple
metadata:
  name: simple
spec:
  # This field is passed to the Helm chart and configures the message that podinfo will show
  message: "bar"
```

Proceed by applying the instance which will create all the resources defined in the `ResourceGraphDefinition`:

```bash
kubectl apply -f instance.yaml
```

This will take some time, but if everything went well, you should see the following output:

```bash
kubectl get simple
```

```console
NAME     STATE    SYNCED   AGE
simple   ACTIVE   True     5m28s
```

and the deployment should be in the state `Available`:

```bash
kubectl get deployments
```

```console
NAME                  READY   UP-TO-DATE   AVAILABLE   AGE
simple-podinfo        1/1     1            1           40m
```

To make sure that the deployment was configured successfully, take a look at the pod itself or execute the following
command:

```bash
kubectl get pods -l app.kubernetes.io/name=simple-podinfo -o jsonpath='{.items[0].spec.containers[0].env[?(@.name=="PODINFO_UI_MESSAGE")].value}'
```

which should return the value you passed in the instance:

```console
bar
```

You now have successfully created an OCM component version containing a Helm chart and deployed as well as configured it
using the OCM K8s Toolkit, kro, and FluxCD.

#### Troubleshooting

One common issue, when using GitHub's container registry, is that the transferred OCM component is by default a
private package. If so, you might see an error like the following:

```console
failed to list versions: failed to list tags: GET "https://ghcr.io/v2...": response status code 401: unauthorized: authentication required
```

You can resolve this issue by making the package public or by [providing credentials](credentials.md) to the
respective resources.

[ocm-doc]: https://ocm.software/docs/getting-started/create-component-version/
[ocm-credentials]: https://ocm.software/docs/tutorials/creds-in-ocmconfig/
