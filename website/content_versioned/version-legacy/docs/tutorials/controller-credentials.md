---
title: Configure Credentials for OCM Controllers
description: "Learn how to configure credentials for accessing private OCM repositories"
icon: "🔑"
weight: 46
toc: true
---

OCM controller resources need access to OCM components and their resources. If these OCM components are stored in a
private OCM repository, we need to configure credentials to allow OCM controller resources to access these
repositories.

## How to configure credentials?

Currently, OCM controllers supports two ways to configure credentials for accessing private OCM repositories:

- [Kubernetes secret of type `dockerconfigjson`](#create-a-kubernetes-secret-of-type-dockerconfigjson)
- [Kubernetes secret or configmap containing an `.ocmconfig` file](#create-a-kubernetes-secret-or-configmap-from-ocmconfig-file)

### Create a Kubernetes secret of type `dockerconfigjson`

If you already have an existing Docker configuration file that you use to access your private OCM repository, you can
create a Kubernetes secret of type `dockerconfigjson` that contains the credentials:

```bash
kubectl create secret docker-registry ocm-secret --from-file=<path-to-your-docker-config-file>
```

{{<callout context="caution" title="Kubernetes secrets" icon="outline/lock-open">}}
Be aware that Kubernetes secrets are only `base64` encoded and not encrypted. This means that anyone with access to the
Kubernetes secret can access the credentials.

Accordingly, you should make sure that the Docker configuration file only contains information required for accessing
the private OCM repository.
{{</callout>}}

In case you want to create the secret manually, you can use the following command to create a Kubernetes secret
of type `dockerconfigjson`:

```bash
kubectl create secret docker-registry ocm-secret \
  --docker-username=<your-name> \
  --docker-password=<your-password> \
  --docker-server=<your-OCM-repository-url>
```

### Create a Kubernetes secret or configmap from `.ocmconfig` file

To create a Kubernetes secret or configmap containing an OCM configuration that allows OCM controller resources
to access private OCM repositories, you can use the `.ocmconfig` file used to transfer the OCM component in the
first place.

{{<callout context="caution" title="OCM Configuration File" icon="outline/lock-password">}}
Usually, the `.ocmconfig` file is located in your HOME directory. However, this `.ocmconfig` could contain more
configurations than just the credentials for accessing private OCM repositories. As this `.ocmconfig` will be used
to create a Kubernetes secret or configmap to which other users might have access to, you have to make sure that it
only contains the configuration you want to share.

We recommend to create a new `.ocmconfig` file that only contains the credentials for accessing the private OCM
repository.

For more information on how to create and use the `.ocmconfig` file, please refer to the
[OCM CLI credentials documentation][ocm-credentials].
{{</callout>}}

For instance, consider you used the following command and `.ocmconfig` file to transfer the OCM component:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: OCIRegistry
          scheme: https
          hostname: ghcr.io
          pathprefix: <your-namespace>
        credentials:
          - type: Credentials
            properties:
              username: <your-username>
              password: <your-password/token>
```

```bash
ocm --config ./.ocmconfig transfer ctf ./ctf ghcr.io/<your-namespace>
```

You can now create a secret in the Kubernetes cluster that contains the `.ocmconfig` file:

```bash
kubectl create secret generic ocm-secret --from-file=./.ocmconfig
```

{{<callout context="note" title="OCM Config Key" icon="outline/menu-deep">}}
Make sure that the secret or configmap containing an OCM config has the correct key to the OCM config file
`.ocmconfig`. This is required for OCM controller resources to be able to read the OCM configuration.
Using the filename `.ocmconfig` in the `--from-file` option takes care of that.
{{</callout>}}

## How to use the configured credentials?

Every OCM controller resource offers a `spec.ocmConfig` field that can be used to specify the credentials for accessing
private OCM repositories. It expects an `OCMConfiguration` that contains a `NamespacedObjectKindReference` to the secret
or configmap that contains the credentials.

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Repository
metadata:
  name: helm-configuration-localization-repository
spec:
  repositorySpec:
    baseUrl: ghcr.io/<your-namespace>
    type: OCIRegistry
  interval: 1m
  ocmConfig:
    - kind: secret
      name: ocm-secret
```

{{<callout context="note" title="Config Propagation" icon="outline/file-settings" >}}
`ocmConfig` is propagated by default and the `Component`, `Resource`, and `Deployer` custom resources will
automatically consume the `ocmConfig` from the custom resource they are referencing if they do not specify their own
`ocmConfig`.
This means that if you specify the `ocmConfig` in a `Repository`, the `Component` that references this
`Repository` will automatically consume the same `ocmConfig` and you do not need to specify it again in the
`Component`.
{{</callout>}}

If you want to consume the `ocmConfig` of a `Repository` in  a `Component` in addition to another `ocmConfig` that is
specified in the `Component`, you need to specify the `ocmConfig` reference to the repository as well:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Repository
metadata:
  name: guide-repository
  namespace: default
spec:
  repositorySpec:
    baseUrl: ghcr.io/<your-namespace>
    type: OCIRegistry
  interval: 1m
  ocmConfig:
    - kind: Secret
      name: ocm-secret
---
apiVersion: delivery.ocm.software/v1alpha1
kind: Component
metadata:
  name: guide-component
spec:
  component: ocm.software/ocm-k8s-toolkit/guide-component
  repositoryRef:
    name: guide-repository
  semver: 1.0.0
  interval: 1m
  ocmConfig:
    - kind: Secret
      name: another-ocm-secret
    - kind: Repository
      apiVersion: delivery.ocm.software/v1alpha1
      name: guide-repository
      namespace: default
```

In some cases, you do not want to propagate the `ocmConfig` of a custom resource. To do so, you can set the `policy` to
`DoNotPropagate`:

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Repository
metadata:
  name: guide-repository
  namespace: default
spec:
  repositorySpec:
    baseUrl: ghcr.io/<your-namespace>
    type: OCIRegistry
  interval: 1m
  ocmConfig:
    - kind: Secret
      name: ocm-secret
      policy: DoNotPropagate
```

[ocm-credentials]: {{< relref "creds-in-ocmconfig.md" >}}
