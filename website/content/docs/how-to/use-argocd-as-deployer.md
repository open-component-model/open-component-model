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

## How Deployers Work in kro

In the OCM controller stack, deployment happens through a `ResourceGraphDefinition` (RGD) managed by
[kro](https://kro.run). The RGD describes a graph of Kubernetes resources. You include the deployer
CRDs — Flux's `HelmRelease` / `OCIRepository`, or Argo CD's `Application` — as resources in that
graph. The OCM `Resource` controller resolves the artifact location and publishes it in its `.status`;
the deployer resource then picks that up via kro's CEL expressions.

This means Argo CD is not a plugin — it is a Kubernetes operator whose CRDs you reference in your RGD.

## Flux vs Argo CD at a Glance

| Feature | Flux | Argo CD |
| --- | --- | --- |
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
            digest: resource.access.toOCI().digest

    # Argo CD Application — replaces Flux OCIRepository + HelmRelease
    - id: argocdApplication
      readyWhen:
        - ${argocdApplication.status.health.status == "Healthy"}
        - ${argocdApplication.status.sync.status == "Synced"}
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

This is supported since Argo CD v3.1. The `digest` field is already exposed via `additionalStatusFields` above.
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
      readyWhen:
        - ${argocdApplication.status.health.status == "Healthy"}
        - ${argocdApplication.status.sync.status == "Synced"}
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
                # Use JSON array form for patches — required when any patch value contains a CEL expression
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

- [Tutorial: Deploy a Helm Chart]({{< relref "../getting-started/deploy-helm-chart.md" >}}) — Flux-based walkthrough of the same scenario
- [Tutorial: Deploy a Helm Chart (Bootstrap)]({{< relref "docs/tutorials/deploy-helm-chart-bootstrap.md" >}}) — Packaging the RGD inside the OCM component
- [Concept: OCM Controllers]({{< relref "ocm-controllers.md" >}})
