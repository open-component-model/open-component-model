---
title: "Deploying the Open Delivery Gear Locally"
description: "Deploy a custom ODG instance locally using KinD."
weight: 2
toc: true
---

## Goal

Deploy a custom Open Delivery Gear (ODG) instance on your local machine using [KinD](https://kind.sigs.k8s.io/).

## You'll end up with

- A local Kubernetes cluster running a full ODG instance
- `delivery-service` forwarded to `http://localhost:5000`
- `delivery-dashboard` forwarded to `http://localhost:3000`

**Estimated time:** ~20 minutes

## Prerequisites

- [Kubectl](https://kubernetes.io/docs/tasks/tools)
- [KinD](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- [Helm](https://helm.sh/docs/intro/install)
- [OCM CLI](https://github.com/open-component-model/open-component-model#ocm-cli)

## Steps

### Configure the values file

Customise ODG according to your needs by adjusting the [values file](https://github.com/open-component-model/open-delivery-gear/blob/main/local-setup/kind/values.yaml).
There are already reasonable defaults available for most entries, however, the following entries must still be provided:

- OCI registry credentials to access desired component descriptors and resources via `secrets.oci-registry` (in case they are not publicly available)
- GitHub credentials via `.secrets.github` or `.secrets.github-app` (both to allow authentication within ODG itself as well to access necessary repositories)
- GitHub App credentials to allow OAuth:
  1. Go to your GitHub organisation's settings
  2. Developer settings -> GitHub Apps -> New GitHub App
  3. Fill in the form ("Callback URL" -> `http://localhost:3000`, "Request user authorisation (OAuth) during installation" -> `True`, other checkboxes -> `False`)
  4. Fill in `client_id`, `client_secret` and desired `role_bindings` via `secrets.oauth-cfg`

### Start up the cluster

To create a local Kubernetes cluster and deploy ODG, run `make kind-up`. If you want to deploy a specific version of ODG, set the environment variable `ODG_VERSION`. Otherwise, the OCM CLI is used to retrieve the greatest version.

{{< callout type="note" >}}
Currently, the listing of available versions via OCM CLI is not working for the OCM component descriptor of ODG. Therefore, an explicit version MUST be set via the `ODG_VERSION` environment variable.
{{< /callout >}}

Upon execution, this command will create `<REPO_ROOT>/local-setup/kind/kubeconfig` which can be used to interact with the ODG cluster.

### Add extensions (optional)

ODG extensions can be dynamically added to your installation. Configure them via `extensions_cfg` in the [values file](https://github.com/open-component-model/open-delivery-gear/blob/main/local-setup/kind/values.yaml).

## Troubleshooting

### Symptom: Deployment fails with version lookup error

**Cause:** OCM CLI version listing is not working for the ODG component descriptor.

**Fix:** Set the `ODG_VERSION` environment variable explicitly before running `make kind-up`.

### Update configuration

To update the ODG deployment after your local configuration has changed, run `make kind-update`. This upgrades the existing Helm charts and re-applies your configuration settings without recreating the KinD cluster.

### Terminate the cluster

To stop ODG and delete the KinD cluster, run `make kind-down`. This does _not_ delete the database storage, which is permanently stored on the host machine. To also clear the database storage, delete the `/var/delivery-db` directory.

## Next steps

- [Set up a hybrid dev setup]({{< relref "docs/how-to/odg/setting-up-a-hybrid-dev-setup/" >}})
- [Prepare your component for ODG]({{< relref "docs/how-to/odg/prepare-your-component-for-odg/" >}})

## Related documentation

- [ODG System Architecture]({{< relref "docs/concepts/odg/odg-system-architecture/" >}})
