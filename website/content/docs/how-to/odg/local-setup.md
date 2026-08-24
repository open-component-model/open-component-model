---
title: "Deploying the Open Delivery Gear Locally"
description: "This guide will help you deploy a custom Open Delivery Gear (ODG) on your local machine using KinD. If you encounter any problems, please feel free to open…"
weight: 20
toc: true
---
This guide will help you deploy a custom Open Delivery Gear (ODG) on your local
machine using [KinD](https://kind.sigs.k8s.io/). If you encounter any problems,
please feel free to [open an issue](https://github.com/open-component-model/open-delivery-gear/issues/new?template=bug.md)
so that we can improve this process or documentation.

## Prerequisites

To get started, you first of all need to install the required toolchain:

- [Kubectl](https://kubernetes.io/docs/tasks/tools)
- [KinD](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- [Helm](https://helm.sh/docs/intro/install)
- [OCM CLI](https://github.com/open-component-model/open-component-model#ocm-cli)

## Configuration

To customise ODG according to your needs, you have to adjust the [values file](https://github.com/open-component-model/open-delivery-gear/blob/main/local-setup/kind/values.yaml).
There are already reasonable defaults available for most entries, however,
following entries must still be provided:

- OCI registry credentials to access desired component descriptors and
resources via `secrets.oci-registry` (in case they are not publicly available)
- GitHub credentials via `.secrets.github` or `.secrets.github-app` (both to
allow authentication within ODG itself as well to access necessary
repositories)
- GitHub App credentials to allow OAuth  
    (1) Go to your GitHub organisation's settings  
    (2) Developer settings -> GitHub Apps -> New GitHub App  
    (3) Fill in the form ("Callback URL" -> `http://localhost:3000`, "Request
    user authorisation (OAuth) during installation" -> `True`, other checkboxes
    -> `False`)  
    (4) Fill in `client_id`, `client_secret` and desired `role_bindings` via
    `secrets.oauth-cfg`  

## Start-Up

To create a local Kubernetes cluster and deploy ODG, you have to run
`make kind-up`. If you want to deploy a specific version of ODG, you have to
set the environment variable `ODG_VERSION`. Otherwise, the OCM CLI is used to
retrieve the greatest version. Upon execution, this command will create
`<REPO_ROOT>/local-setup/kind/kubeconfig` which can be used to interact with
the ODG cluster. Also, it will forward the delivery-service to
`http://localhost:5000` and the delivery-dashboard to `http://localhost:3000`.

> [!NOTE]  
> Currently, the listing of available versions via OCM CLI is not working for
> the OCM component descriptor of ODG. Therefore, an explicit version MUST be
> set via the `ODG_VERSION` environment variable.

## Configuration Update

To update the ODG deployment in case your local configuration has changed, just
run the `make kind-update` command. This will upgrade the existing Helm charts
and re-apply your configuration settings without the need to re-create your
KinD cluster.

## Termination

If you wish to stop ODG and delete the KinD cluster, you have to run
`make kind-down`. However, this will _not_ delete the database storage since it
is permanently stored on the host machine. To also clear the database storage,
you have to delete the `/var/delivery-db` directory.

## Extensions

ODG extensions can be dynamically added to your installation. Therefore, the
configuration of the extensions must be done via `extensions_cfg` in the
[values file](https://github.com/open-component-model/open-delivery-gear/blob/main/local-setup/kind/values.yaml).
