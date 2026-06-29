---
title: "Use Argo CD as a Deployer"
description: "Deploy OCM-managed Helm charts and Kustomize overlays to Kubernetes using Argo CD as the GitOps deployer."
icon: "🚀"
weight: 37
toc: true
---

This guide shows how to use [Argo CD](https://argo-cd.readthedocs.io/en/stable/) as the deployer in an OCM controller
setup, as an alternative to Flux. Argo CD and Flux are peer options — you express the deployer by including the
appropriate resource in your `ResourceGraphDefinition`.

## Estimated time

~10 minutes

## Prerequisites

- [Controller environment]({{< relref "setup-controller-environment.md" >}}) set up with OCM Controllers, kro, and
  Argo CD installed (follow the **Argo CD** tab in the
  [Install a Deployer]({{< relref "setup-controller-environment.md" >}}#install-a-deployer) section)
- [Custom RBAC]({{< relref "custom-rbac.md" >}}) configured to allow the controller to manage `ResourceGraphDefinitions`
- [OCM CLI]({{< relref "ocm-cli-installation.md" >}}) installed
- Access to an OCI registry

## Install Argo CD

If Argo CD is not yet installed on your cluster, follow these steps. Skip this section if it is already running.

```shell
kubectl create namespace argocd
kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml
```

<details>
<summary>You should see this output</summary>

```text
namespace/argocd created
customresourcedefinition.apiextensions.k8s.io/applications.argoproj.io created
customresourcedefinition.apiextensions.k8s.io/appprojects.argoproj.io created
serviceaccount/argocd-application-controller created
serviceaccount/argocd-applicationset-controller created
serviceaccount/argocd-dex-server created
serviceaccount/argocd-notifications-controller created
serviceaccount/argocd-redis created
serviceaccount/argocd-repo-server created
serviceaccount/argocd-server created
role.rbac.authorization.k8s.io/argocd-application-controller created
...
[59 resources created in total]
```
</details>
<br>

Wait for all pods to become ready:

```shell
kubectl wait --for=condition=Ready pods --all -n argocd --timeout=120s
```

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

Enable OCI Helm support and restart the repo server:

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

## How Deployers Work in kro

In the OCM controller stack, deployment happens through a `ResourceGraphDefinition` (RGD) managed by
[kro](https://kro.run). The RGD describes a graph of Kubernetes resources. You include the deployer's
own CRD — Flux's `HelmRelease` / `OCIRepository`, or Argo CD's `Application` — as a resource in that
graph. The OCM `Resource` controller resolves the artifact location and publishes it in its `.status`;
the deployer resource then picks that up via kro's CEL expressions.

This means Argo CD is not a plugin — it is a Kubernetes operator whose CRDs you reference in your RGD.

## Flux vs Argo CD at a Glance

| | Flux | Argo CD |
|---|---|---|
| Helm source type | `OCIRepository` + `HelmRelease` | `Application` with inline OCI source |
| Helm digest pinning | `spec.ref.digest` | `spec.source.targetRevision: sha256:<digest>` (since v3.1) |
| Helm value injection | `HelmRelease.spec.values` (string) | `Application.spec.source.helm.valuesObject` (structured YAML) |
| Kustomize + CEL patches | YAML block scalars | JSON arrays (kro constraint — applies to both deployers) |
| Operator namespace | `flux-system` | `argocd` |

{{< callout context="note" title="Kustomize patches and CEL" icon="outline/info-circle" >}}
When a kro CEL expression (`${…}`) appears inside a Kustomize patch, kro cannot parse multi-line YAML
block scalars. Use JSON arrays for the patch value instead. This is a kro constraint that affects both
Flux and Argo CD equally — it is not Argo CD-specific.
{{< /callout >}}

## Deploy a Helm Chart

### ResourceGraphDefinition

The Argo CD `Application` resource replaces Flux's `OCIRepository` + `HelmRelease` pair. Everything
else in the RGD — `Repository`, `Component`, `Resource` — is identical.

```yaml
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: helm-simple
spec:
  schema:
    apiVersion: v1alpha1
    kind: HelmSimple
    spec:
      prefix: string | default="helm-simple"
      message: string | default="hello"
  resources:
    - id: repository
      readyWhen:
        - ${repository.status.conditions.exists(c, c.type == 'Ready' && c.status == 'True')}
      template:
        apiVersion: delivery.ocm.software/v1alpha1
        kind: Repository
        metadata:
          name: "${schema.spec.prefix}-repository"
        spec:
          repositorySpec:
            baseUrl: $OCM_REPO
            type: OCIRegistry
          interval: 1m

    - id: component
      readyWhen:
        - ${component.status.conditions.exists(c, c.type == 'Ready' && c.status == 'True')}
      template:
        apiVersion: delivery.ocm.software/v1alpha1
        kind: Component
        metadata:
          name: "${schema.spec.prefix}-component"
        spec:
          repositoryRef:
            name: ${repository.metadata.name}
          component: ocm.software/ocm-k8s-toolkit/simple
          semver: 1.0.0
          interval: 1m

    - id: resourceChart
      readyWhen:
        - ${resourceChart.status.conditions.exists(c, c.type == 'Ready' && c.status == 'True')}
      template:
        apiVersion: delivery.ocm.software/v1alpha1
        kind: Resource
        metadata:
          name: "${schema.spec.prefix}-resource"
        spec:
          componentRef:
            name: ${component.metadata.name}
          resource:
            byReference:
              resource:
                name: helm-resource
          additionalStatusFields:
            registry: resource.access.toOCI().registry
            repository: resource.access.toOCI().repository
            tag: resource.access.toOCI().tag

    # Argo CD Application — replaces Flux OCIRepository + HelmRelease
    - id: argocdApplication
      template:
        apiVersion: argoproj.io/v1alpha1
        kind: Application
        metadata:
          name: "${schema.spec.prefix}"
          namespace: argocd
          finalizers:
            - resources-finalizer.argocd.argoproj.io
        spec:
          project: default
          source:
            chart: podinfo
            repoURL: oci://${resourceChart.status.additional.registry}/${resourceChart.status.additional.repository}
            targetRevision: ${resourceChart.status.additional.tag}
            helm:
              releaseName: "${schema.spec.prefix}"
              valuesObject:
                ui:
                  message: ${schema.spec.message}
          destination:
            server: https://kubernetes.default.svc
            namespace: default
          syncPolicy:
            automated:
              prune: true
              selfHeal: true
            syncOptions:
              - CreateNamespace=true
```

{{< callout context="tip" title="Digest pinning" icon="outline/shield-check" >}}
To pin the Helm chart to an immutable digest instead of a tag, replace `targetRevision` with the digest:

```yaml
targetRevision: sha256:<digest-from-resourceChart.status.additional.digest>
```

This is supported since Argo CD v3.1. Expose the digest in `additionalStatusFields` the same way as `tag`.
{{< /callout >}}

### Apply and Verify

```shell
envsubst < rgd.yaml | kubectl apply -f -
```

Create an instance:

```yaml
apiVersion: kro.run/v1alpha1
kind: HelmSimple
metadata:
  name: my-app
spec:
  prefix: my-app
  message: "Deployed with OCM and Argo CD!"
```

```shell
kubectl apply -f instance.yaml
```

Check the Argo CD Application status:

```shell
kubectl get applications -n argocd
```

## Localization (Helm Values)

When an OCM component bundles both a Helm chart and an image reference, kro CEL expressions inject
the resolved image coordinates into the Argo CD `valuesObject`. This avoids the escaping issues of
the string-based `values` field.

```yaml
    - id: argocdApplication
      template:
        ...
        spec:
          source:
            helm:
              valuesObject:
                image:
                  repository: ${resourceImage.status.additional.registry}/${resourceImage.status.additional.repository}
                  tag: ${resourceImage.status.additional.tag}
                ui:
                  message: ${schema.spec.message}
```

See `kubernetes/controller/examples/helm-configuration-localization/rgd.yaml` for the full example.

## Deploy a Kustomize Overlay

For Kustomize sources, Argo CD uses a single `Application` resource with `spec.source.path` pointing
to the overlay directory. The Git commit SHA comes from the OCM Resource's status.

```yaml
    - id: argocdApplication
      template:
        apiVersion: argoproj.io/v1alpha1
        kind: Application
        metadata:
          name: "${schema.spec.prefix}"
          namespace: argocd
          finalizers:
            - resources-finalizer.argocd.argoproj.io
        spec:
          project: default
          source:
            repoURL: ${resourceKustomization.status.resource.access.repoUrl}
            targetRevision: ${resourceKustomization.status.resource.access.commit}
            path: "kustomize"
            kustomize:
              patches:
                # JSON array form required when the patch contains a CEL expression (kro constraint)
                - patch: |
                    - op: replace
                      path: /metadata/name
                      value: my-app-podinfo
                  target:
                    kind: Deployment
                    name: podinfo
          destination:
            server: https://kubernetes.default.svc
            namespace: default
          syncPolicy:
            automated:
              prune: true
              selfHeal: true
            syncOptions:
              - CreateNamespace=true
```

{{< callout context="caution" title="CEL expressions in Kustomize patches" icon="outline/alert-triangle" >}}
Static patches (no `${…}` CEL references) can use regular YAML block scalars. Patches that contain
CEL expressions must use the JSON array form shown above — kro cannot parse multi-line YAML block
scalars containing `${…}` references. This applies to both Argo CD and Flux.
{{< /callout >}}

## Troubleshooting

### Argo CD Application stuck in `OutOfSync`

- Verify Argo CD has OCI Helm support enabled: check `argocd-cmd-params-cm` for
  `application.helm.enableOCI: "true"`.
- Confirm the `Application` namespace is `argocd` and the destination namespace exists or
  `CreateNamespace=true` is set in `syncOptions`.

### CEL expressions not resolving

- The OCM `Resource` must be in `Ready` state before kro propagates its status into the `Application`.
  Add a `readyWhen` condition on the `resourceChart` resource if it is missing.

## Related

- [Tutorial: Deploy a Helm Chart]({{< relref "deploy-helm-chart.md" >}}) — Flux-based walkthrough of the same scenario
- [Tutorial: Deploy a Helm Chart (Bootstrap)]({{< relref "docs/tutorials/deploy-helm-chart-bootstrap.md" >}}) — Packaging the RGD inside the OCM component
- [Concept: OCM Controllers]({{< relref "ocm-controllers.md" >}})
