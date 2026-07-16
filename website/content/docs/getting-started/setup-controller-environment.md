---
title: "Set up Controller Environments"
description: "Set up a local Kubernetes environment with the OCM Controllers, kro, and Flux for testing OCM deployments."
icon: "⚓"
weight: 25
toc: true
---

This guide helps you set up a local Kubernetes environment for testing OCM controller-based deployments.
You'll install the OCM Controllers, kro, and Flux to enable GitOps workflows with OCM component versions.

{{< callout context="tip" title="Not all components are always required" icon="outline/info-circle" >}}
Depending on your use case, you may not need the full setup. For example, if 
you're only deploying raw k8s deployment from an ocm resource, you may be 
able to skip kro and Flux. 

Check the prerequisites of the tutorial or how-to you're following to see what's 
actually needed. This guide installs everything so you're covered for any 
scenario.
{{< /callout >}}

## You'll end up with

- A local or remote Kubernetes cluster with OCM Controllers, kro, and Flux installed

## Estimated time

~15 minutes

## Prerequisites

- [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) installed
- [Helm](https://helm.sh/docs/intro/install/) installed
- [kind](https://kind.sigs.k8s.io/docs/user/quick-start) installed (or access to a remote Kubernetes cluster)
- [OCM CLI]({{< relref "ocm-cli-installation.md" >}}) installed
- Access to an OCI registry (e.g., [ghcr.io](https://docs.github.com/en/packages/learn-github-packages/introduction-to-github-packages))
- **Deployer:** [Flux CLI](https://fluxcd.io/flux/installation/#install-the-flux-cli) (if using Flux) or [ArgoCD](https://argo-cd.readthedocs.io/en/stable/getting_started/#1-install-argo-cd) (if using Argo CD)

## Setup Workflow

{{< steps >}}
{{< step >}}

### Create a Local Kubernetes Cluster

{{< callout icon="outline/info-circle" >}}
Skip this step if you're using a remote Kubernetes cluster.
{{< /callout >}}

Create a local kind cluster:

```shell
kind create cluster
```

<details>
<summary>You should see this output</summary>

```text
Creating cluster "kind" ...
 ✓ Ensuring node image (kindest/node:v1.35.0) 🖼
 ✓ Preparing nodes 📦
 ✓ Writing configuration 📜
 ✓ Starting control-plane 🕹️
 ✓ Installing CNI 
 ✓ Installing StorageClass 💾
Set kubectl context to "kind-kind"
You can now use your cluster with:

kubectl cluster-info --context kind-kind
Have a nice day! 👋
```
</details>
<br>

Verify the cluster is running:

```shell
kubectl cluster-info
```
<details>
<summary>You should see this output</summary>

```text
Kubernetes control plane is running at https://127.0.0.1:53348
CoreDNS is running at https://127.0.0.1:53348/api/v1/namespaces/kube-system/services/kube-dns:dns/proxy

To further debug and diagnose cluster problems, use 'kubectl cluster-info dump'
```
</details>

{{< /step >}}
{{< step >}}

### Install the OCM Controllers

Use Helm to install the OCM controllers:

```bash
helm install ocm-k8s-toolkit "oci://ghcr.io/open-component-model/kubernetes/controller/chart" \
  --namespace ocm-k8s-toolkit-system \
  --create-namespace
```

{{<callout context="note" title="Resource names follow the release name" icon="outline/info-circle">}}
The release name `ocm-k8s-toolkit` used above gives the controller's resources predictable names, such as the service account `ocm-k8s-toolkit-controller-manager`. If you install under a different release name — or via a GitOps tool such as Flux that alters the effective release name — add `--set fullnameOverride=ocm-k8s-toolkit` to keep these names stable. This matters when you [configure custom RBAC]({{< relref "/docs/how-to/custom-rbac.md" >}}), which binds to the service account by name.
{{</callout>}}

<details>
<summary>You should see this output</summary>

```text
Pulled: ghcr.io/open-component-model/kubernetes/controller/chart:0.4.0
Digest: sha256:eac0dc587a1d288f36ef1961bb69f0ffb2791e0153f86d1fdbe54ae2f36f1194
NAME: ocm-k8s-toolkit
LAST DEPLOYED: Tue Apr 28 17:42:51 2026
NAMESPACE: ocm-k8s-toolkit-system
STATUS: deployed
REVISION: 1
DESCRIPTION: Install complete
TEST SUITE: None
```
</details>
<br>

Verify the OCM controller is running:

```shell
kubectl get pods -n ocm-k8s-toolkit-system
```

<details>
<summary>You should see this output</summary>

```text
NAME                                                  READY   STATUS    RESTARTS   AGE
ocm-k8s-toolkit-controller-manager-79b7975755-vxtqt   1/1     Running   0          59s
```
</details>

{{< /step >}}
{{< step >}}

## Install kro

Install [kro](https://kro.run) following the [official installation guide](https://kro.run/docs/getting-started/Installation). The easiest way is via Helm:

```shell
helm install kro oci://registry.k8s.io/kro/charts/kro \
  --namespace kro-system \
  --create-namespace
```

{{< callout context="caution" title="Security consideration" icon="outline/alert-triangle" >}}
This default installation grants kro cluster-wide access to all resources, which is suitable for local development but not recommended for production environments. See the [kro documentation](https://kro.run/next/docs/advanced/access-control) for guidance on configuring more restrictive RBAC.
{{< /callout >}}
<details>
<summary>You should see this output</summary>

```text
Pulled: registry.k8s.io/kro/charts/kro:0.8.5
Digest: sha256:c9a9dc0133f43a25711f4bdbce1eeb4b6448015958f901c6fad61a049e54415e
NAME: kro
LAST DEPLOYED: Wed Feb 25 12:02:15 2026
NAMESPACE: kro-system
STATUS: deployed
REVISION: 1
DESCRIPTION: Install complete
TEST SUITE: None
```
</details>
<br>

Verify kro is running:

```shell
kubectl get pods -n kro-system
```

<details>
<summary>You should see this output</summary>

```text
NAME                   READY   STATUS    RESTARTS   AGE
kro-5644d5759f-82nsx   1/1     Running   0          2m22s
```
</details>

{{< /step >}}
{{< step >}}

### Install a Deployer

The following examples of [Flux](https://fluxcd.io/) and [Argo CD](https://argo-cd.readthedocs.io/en/stable/) are demonstrating how they can be used as deployers.  In theory, you could use any other deployer that is able to apply a deployable resource to a Kubernetes cluster.

Choose the one you're already using or prefer.

{{< tabs "deployer-install" >}}
{{< tab "Flux" >}}

Install the controllers using the Flux CLI:

```shell
flux install
```

<details>
<summary>You should see this output</summary>

```text
✚ generating manifests
✔ manifests build completed
► installing components in flux-system namespace
CustomResourceDefinition/alerts.notification.toolkit.fluxcd.io created
CustomResourceDefinition/buckets.source.toolkit.fluxcd.io created
CustomResourceDefinition/externalartifacts.source.toolkit.fluxcd.io created
CustomResourceDefinition/gitrepositories.source.toolkit.fluxcd.io created
CustomResourceDefinition/helmcharts.source.toolkit.fluxcd.io created
CustomResourceDefinition/helmreleases.helm.toolkit.fluxcd.io created
CustomResourceDefinition/helmrepositories.source.toolkit.fluxcd.io created
CustomResourceDefinition/kustomizations.kustomize.toolkit.fluxcd.io created
CustomResourceDefinition/ocirepositories.source.toolkit.fluxcd.io created
CustomResourceDefinition/providers.notification.toolkit.fluxcd.io created
CustomResourceDefinition/receivers.notification.toolkit.fluxcd.io created
Namespace/flux-system created
ClusterRole/crd-controller-flux-system created
ClusterRole/flux-edit-flux-system created
ClusterRole/flux-view-flux-system created
ClusterRoleBinding/cluster-reconciler-flux-system created
ClusterRoleBinding/crd-controller-flux-system created
ResourceQuota/flux-system/critical-pods-flux-system created
ServiceAccount/flux-system/helm-controller created
ServiceAccount/flux-system/kustomize-controller created
ServiceAccount/flux-system/notification-controller created
ServiceAccount/flux-system/source-controller created
Service/flux-system/notification-controller created
Service/flux-system/source-controller created
Service/flux-system/webhook-receiver created
Deployment/flux-system/helm-controller created
Deployment/flux-system/kustomize-controller created
Deployment/flux-system/notification-controller created
Deployment/flux-system/source-controller created
NetworkPolicy/flux-system/allow-egress created
NetworkPolicy/flux-system/allow-scraping created
NetworkPolicy/flux-system/allow-webhooks created
◎ verifying installation
✔ helm-controller: deployment ready
✔ kustomize-controller: deployment ready
✔ notification-controller: deployment ready
✔ source-controller: deployment ready
✔ install finished
```
</details>
<br>

Verify Flux is running:

```shell
kubectl get pods -n flux-system
```

<details>
<summary>You should see this output</summary>

```text
NAME                                         READY   STATUS      RESTARTS        AGE
helm-controller-b6767d66-zbwws               1/1     Running     0               3h29m
kustomize-controller-57c7ff5596-v6fvr        1/1     Running     0               3h29m
notification-controller-58ffd586f7-pr65t     1/1     Running     0               3h29m
source-controller-6ff87cb475-2h2lv           1/1     Running     0               3h29m
```
</details>

{{< /tab >}}
{{< tab "Argo CD" >}}

[Install](https://argo-cd.readthedocs.io/en/stable/operator-manual/installation/#helm) Argo CD via [Helm Chart](https://github.com/argoproj/argo-helm/tree/main/charts/argo-cd#installing-the-chart):

```shell
helm repo add argo https://argoproj.github.io/argo-helm
# ... "argo" has been added to your repositories

helm repo update argo

helm upgrade --install argocd argo/argo-cd \
  --namespace argocd \
  --create-namespace \
  --wait \
  --timeout 5m
# ...

```

Wait for all pods to become ready:

```shell
kubectl wait --for=condition=Ready pods --all -n argocd --timeout=120s
```

{{<callout context="note" title="Resource names follow the release name" icon="outline/info-circle">}}
The release name `ocm-k8s-toolkit` used above gives the controller's resources predictable names, such as the service account `ocm-k8s-toolkit-controller-manager`. If you install under a different release name — or via a GitOps tool such as Flux that alters the effective release name — add `--set fullnameOverride=ocm-k8s-toolkit` to keep these names stable. This matters when you [configure custom RBAC]({{< relref "/docs/how-to/custom-rbac.md" >}}), which binds to the service account by name.
{{</callout>}}

<details>
<summary>You should see this output</summary>

```text
pod/argocd-application-controller-0 condition met
pod/argocd-applicationset-controller-68fd97ccb6-nkcbg condition met
pod/argocd-dex-server-99ff57675-9qk2l condition met
pod/argocd-notifications-controller-8596549fb6-bldjz condition met
pod/argocd-redis-6f6867546c-vs6c6 condition met
pod/argocd-repo-server-59444f4bbb-gbzxn condition met
pod/argocd-server-765575f778-j8krk condition met
```
</details>
<br>

Verify Argo CD is running:

```shell
kubectl get pods -n argocd
```

<details>
<summary>You should see this output</summary>

```text
NAME                                                READY   STATUS    RESTARTS   AGE
argocd-application-controller-0                     1/1     Running   0          42s
argocd-applicationset-controller-68fd97ccb6-nkcbg   1/1     Running   0          43s
argocd-dex-server-99ff57675-9qk2l                   1/1     Running   0          43s
argocd-notifications-controller-8596549fb6-bldjz    1/1     Running   0          43s
argocd-redis-6f6867546c-vs6c6                       1/1     Running   0          42s
argocd-repo-server-59444f4bbb-gbzxn                 1/1     Running   0          42s
argocd-server-765575f778-j8krk                      1/1     Running   0          42s
```
</details>
<br>

Enable OCI Helm support — required for deploying Helm charts from OCI registries:

```shell
kubectl patch configmap argocd-cmd-params-cm -n argocd \
  --type merge -p '{"data":{"application.helm.enableOCI":"true"}}'
kubectl rollout restart deployment argocd-repo-server -n argocd
kubectl rollout status deployment argocd-repo-server -n argocd --timeout=60s
```

<details>
<summary>You should see this output</summary>

```text
configmap/argocd-cmd-params-cm patched
deployment.apps/argocd-repo-server restarted
Waiting for deployment "argocd-repo-server" rollout to finish: 1 old replicas are pending termination...
Waiting for deployment "argocd-repo-server" rollout to finish: 1 old replicas are pending termination...
deployment "argocd-repo-server" successfully rolled out
```
</details>

{{< /tab >}}
{{< /tabs >}}

{{< /step >}}

{{< step >}}

### Verify Complete Setup

Check all components are running:

```shell
kubectl get pods --all-namespaces | grep -E '(kro-system|flux-system|argocd|ocm-k8s-toolkit-system)'
```

<details>
<summary>You should see this output</summary>

Depending on what deployer you installed the output could differ:

```text
NAMESPACE                NAME                                                 READY    STATUS             RESTARTS        AGE
argocd-application-controller-0                                                1/1     Running            0               24m
argocd-applicationset-controller-85f58f44f4-g85xv                              1/1     Running            0               24m
argocd-dex-server-69884b6f8b-vf5s9                                             1/1     Running            0               24m
argocd-notifications-controller-8669567fb-47fcj                                1/1     Running            0               24m
argocd-redis-5d9668fdff-v2pbp                                                  1/1     Running            0               24m
argocd-repo-server-95465d997-mbmcp                                             1/1     Running            0               24m
argocd-server-767b9d54cf-q7bbz                                                 1/1     Running            0               24m
flux-system              helm-controller-b6767d66-zbwws                        1/1     Running            0               3h39m
flux-system              kustomize-controller-57c7ff5596-v6fvr                 1/1     Running            0               3h39m
flux-system              notification-controller-58ffd586f7-pr65t              1/1     Running            0               3h39m
flux-system              source-controller-6ff87cb475-2h2lv                    1/1     Running            0               3h39m
kro-system               kro-86d5b5b5bd-6gmvr                                  1/1     Running            0               3h38m
ocm-k8s-toolkit-system   ocm-k8s-toolkit-controller-manager-788f58d4bd-ntbx8   1/1     Running            0               57s
```
</details>

{{< /step >}}
{{< /steps >}}

## Registry Access

The OCM Controllers need access to an OCI registry to fetch component versions.

{{< callout context="tip" title="Tip" icon="outline/rocket" >}}
We recommend using a publicly accessible registry like [ghcr.io](https://docs.github.com/en/packages/learn-github-packages/introduction-to-github-packages).
Using a local registry requires additional configuration to ensure it's accessible both from your CLI and from within the cluster.
{{< /callout >}}

For private registries, you'll need to configure credentials. See [Configure Credentials for Private Registries]({{< relref "/docs/how-to/configure-multiple-credentials.md" >}}) for details.

## Cleanup

To remove the local kind cluster after testing, run the following command.
If you plan to continue with the next tutorial steps, you can keep the cluster.

```shell
kind delete cluster
```

<details>
<summary>You should see this output</summary>

```text
Deleting cluster "kind" ...
Deleted nodes: ["kind-control-plane"]
```
</details>

## Next Steps

- [How-to: Deploy Manifests with Deployer]({{< relref "/docs/how-to/deploy-manifests-with-deployer.md" >}}) - Deploy raw Kubernetes manifests without kro or Flux
- [Tutorial: Deploy a Helm Chart]({{< relref "deploy-helm-chart.md" >}}) - Learn to deploy Helm charts using OCM Controllers with kro and Flux

## Related Documentation

- [Concept: OCM Controllers]({{< relref "ocm-controllers.md" >}}) - Learn about the architecture and capabilities of the OCM Controllers
