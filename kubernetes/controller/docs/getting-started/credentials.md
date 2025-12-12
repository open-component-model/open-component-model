---
title: Configuring credentials
description: "Learn how to configure credentials for accessing private OCM repositories in OCM K8s Toolkit resources."
icon: ":key:"
weight:
toc: true
---

# Configuring credentials

OCM K8s Toolkit resources need access to OCM components and their resources. If these OCM components are stored in a
private OCM repository, we need to configure credentials to allow OCM K8s Toolkit resources to access these
repositories.

## How to configure credentials?

Currently, OCM K8s Toolkit supports two ways to configure credentials for accessing private OCM repositories:

- [Kubernetes secret of type `dockerconfigjson`](#create-a-kubernetes-secret-of-type-dockerconfigjson-to-access-private-ocm-repositories)
- [Kubernetes secret or configmap containing an `.ocmconfig` file](#create-a-kubernetes-secret-of-configmap-containing-an-ocm-configuration-to-access-private-ocm-repositories)

### Create a Kubernetes secret of type `dockerconfigjson` to access private OCM repositories

If you already have an existing Docker configuration file that you use to access your private OCM repository, you can
create a Kubernetes secret of type `dockerconfigjson` that contains the credentials:

```bash
kubectl create secret docker-registry ocm-secret --from-file=<path-to-your-docker-config-file>
```

> [!NOTE]
> Be aware that Kubernetes secret are only `base64` encoded and not encrypted. This means that anyone with access to the
> Kubernetes secret can access the credentials.
>
> Accordingly, you should make sure that the Docker configuration files does not contain any other information than the
> credentials for accessing the private OCM repository.

Or in case you want to create the secret manually, you can use the following command to create a Kubernetes secret
of type `dockerconfigjson`:

```bash
kubectl create secret docker-registry ocm-secret \
  --docker-username=<your-name> \
  --docker-password=<your-password> \
  --docker-server=<your-OCM-repository-url>
```

### Create a Kubernetes secret of configmap containing an OCM configuration to access private OCM repositories

To create a Kubernetes secret or configmap containing an OCM configuration that allows OCM K8s Toolkit resources
to access private OCM repositories, you can use the `.ocmconfig` file that was used to transfer the OCM component in the
first place.

> [!NOTE]
> Usually, the `.ocmconfig` file is located in your home directory. However, this `.ocmconfig` could contain more
> configurations than just the credentials for accessing private OCM repositories. As this `.ocmconfig` will be used
> to create a Kubernetes secret or configmap to which other users might have access to, you have to make sure that it
> only contains the configuration you want to share.
>
> We recommend to create a new `.ocmconfig` file that only contains the credentials for accessing the private OCM
> repository.
>
> For more information on how to create and use the `.ocmconfig` file, please refer to the
> [OCM CLI credentials documentation][ocm-credentials].

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

> [!IMPORTANT]
> Make sure that the secret or configmap containing an OCM config has the correct key to the OCM config file
> `.ocmconfig`. This is required for OCM K8s Toolkit resources to be able to read the OCM configuration.
> Using the filename `.ocmconfig` in the `--from-file` option takes care of that.

## How to use the configured credentials?

Every OCM K8s Toolkit resource offers a `spec.ocmConfig` field that can be used to specify the credentials for accessing
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
    - kind: Secret
      name: ocm-secret
```

By default, the `ocmConfig` of a resource is propagated and can be consumed by other resources. So, instead of
specifying the secret or configmap again, you can reference the resource in the `ocmConfig` field:

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
    - kind: Repository
      apiVersion: delivery.ocm.software/v1alpha1
      name: guide-repository
      namespace: default
```

The above example shows how to use the `ocmConfig` field in an `Repository` and a `Component`. The `Repository`
references a secret named `ocm-secret` that contains the credentials for accessing the private OCM repository.
The `Component` then references the `Repository` in `ocmConfig`and uses the same credentials.

However, you always need to specify a reference to the credentials either as secret, configmap, or as OCM K8s Toolkit
resource for *each resource*. The credentials will not be propagated automatically to all OCM K8s Toolkit resources in
the cluster.

In some cases, you do not want to propagate the `ocmConfig` of a resource. To do so, you can set the `policy` to
`DoNotPropagate`:

```yaml
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
    - kind: Repository
      apiVersion: delivery.ocm.software/v1alpha1
      name: guide-repository
      namespace: default
      policy: DoNotPropagate
```

[ocm-credentials]: https://ocm.software/docs/tutorials/creds-in-ocmconfig/
