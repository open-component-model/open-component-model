---
title: Set Up OCM Controller Environment
description: "Set up a local environment for running examples from the getting-started guides"
icon: "🛠️"
weight: 27
toc: true
---

This document describes how to set up a local environment to [Deploy a Helm Chart]({{< relref "deploy-helm-chart.md" >}}) using the OCM controllers.

## Prerequisites

- [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl)

## Start a Local Kubernetes Cluster with kind

{{<callout context="note" title="Kubernetes Cluster" icon="outline/box">}}
You don't need to run kind if you are using a remote Kubernetes cluster you have access to. If so, you can skip this.
{{</callout>}}

For download and installation instructions, see the
[kind documentation](https://kind.sigs.k8s.io/docs/user/quick-start).

To create a local kind cluster run the following command:

```bash
kind create cluster
```

## Install kro

Please follow the official installation guides for [kro](https://kro.run/docs/getting-started/Installation). You might
need [helm](https://helm.sh/docs/intro/install/) to install kro.

If kro is installed correctly, you should see some similar output when running the following command:

```bash
kubectl get pods --all-namespaces
```

```console
NAMESPACE            NAME                                         READY   STATUS             RESTARTS        AGE
...
kro                  kro-86d5b5b5bd-6gmvr                         1/1     Running            0               3h28m
...
```

## Install a Deployer

Currently, we created our examples and getting-started guides using [FluxCD](https://fluxcd.io/) as deployer.
But, in theory, you could use any other deployer that is able to apply a deployable resource to a Kubernetes cluster,
for instance [ArgoCD](https://argo-cd.readthedocs.io/en/stable/).

To install FluxCD, please follow the official [installation guide](https://fluxcd.io/docs/installation/). After you
installed the CLI tool, you can run the following command to install the FluxCD controllers:

```bash
flux install
```

If the FluxCD controllers are installed correctly, you should see some similar output when running the following
command:

```bash
kubectl get pods --all-namespaces
```

```console
NAMESPACE            NAME                                         READY   STATUS             RESTARTS        AGE
...
flux-system          helm-controller-b6767d66-zbwws               1/1     Running            0               3h29m
flux-system          kustomize-controller-57c7ff5596-v6fvr        1/1     Running            0               3h29m
flux-system          notification-controller-58ffd586f7-pr65t     1/1     Running            0               3h29m
flux-system          source-controller-6ff87cb475-2h2lv           1/1     Running            0               3h29m
...
kro                  kro-86d5b5b5bd-6gmvr                         1/1     Running            0               3h28m
...
```

## Install the OCM Controllers

To install the OCM controllers, execute the following command:

```bash
helm install ocm-k8s-toolkit "oci://ghcr.io/open-component-model/kubernetes/controller/chart:<pre-release>" \
  --namespace ocm-k8s-toolkit-system \
  --create-namespace
```

You can check out this [list](https://github.com/open-component-model/open-component-model/pkgs/container/kubernetes%2Fcontroller%2Fchart/versions?filters%5Bversion_type%5D=tagged)
to pick the latest pre-release version until an official release was published.

For local development, you can install directly from the source:

```bash
# In the ocm mono-repo root directory
cd kubernetes/controller
task helm/install
```

If the OCM controllers are installed correctly, you should see some similar output when running the
following command:

```bash
kubectl get pods --all-namespaces
```

```console
NAMESPACE            NAME                                         READY   STATUS             RESTARTS        AGE
...
flux-system              helm-controller-b6767d66-zbwws                        1/1     Running            0               3h39m
flux-system              kustomize-controller-57c7ff5596-v6fvr                 1/1     Running            0               3h39m
flux-system              notification-controller-58ffd586f7-pr65t              1/1     Running            0               3h39m
flux-system              source-controller-6ff87cb475-2h2lv                    1/1     Running            0               3h39m
...
kro                      kro-86d5b5b5bd-6gmvr                                  1/1     Running            0               3h38m
...
ocm-k8s-toolkit-system   ocm-k8s-toolkit-controller-manager-788f58d4bd-ntbx8   1/1     Running            0               57s
...
```

## Access to a Registry

As all examples and guides will create an OCM component version that will be consumed by the OCM controllers, you will
need access to a registry. You can either choose a public registry like [ghcr.io][ghcr.io] or deploy a registry (like
[`registry`][registry], [`zot`][zot], ...) into your Kubernetes cluster.

{{<callout context="caution" title="Registry Access" icon="outline/package-import">}}
If you choose to deploy a registry into your Kubernetes cluster, you have to make sure it is accessible from outside
the cluster (for `ocm transfer` to work) and inside the cluster (for the OCM controllers to work).

We **strongly** recommend using a registry that is publicly accessible, like [ghcr.io][ghcr.io].
Deploying your own registry requires a lot of additional configuration. Especially, if you want to try out the
localization example, you will need to configure a registry that is accessible with the same address from your
CLI, kubelet, and inside the cluster.
{{</callout>}}

---

If you completed all of the above steps, you are ready to go. You can now start with the [Deploy Helm Chart]({{< relref "deploy-helm-chart.md" >}}) example
or play around in the [`examples/`](https://github.com/open-component-model/open-component-model/tree/main/kubernetes/controller/examples) directory.

[ghcr.io]: https://docs.github.com/en/packages/learn-github-packages/introduction-to-github-packages
[registry]: https://hub.docker.com/_/registry
[zot]: https://zotregistry.dev/
