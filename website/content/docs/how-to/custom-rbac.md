---
title: Configure Custom RBAC for Deployers
description: "How to grant the OCM Kubernetes Controller Toolkit additional RBAC permissions required by your deployer targets (e.g. kro ResourceGraphDefinitions)."
weight: 115
toc: true
---

The OCM Kubernetes Controller Toolkit ships with the minimum RBAC permissions needed to manage its own custom resources
(`Repository`, `Component`, `Resource`, `Deployer`). It does **not** include permissions for third-party resources
that your deployers may create or manage.

If your `Deployer` resources produce custom resources (e.g. kro `ResourceGraphDefinitions`), you must grant the
controller's service account the necessary permissions yourself.

## Prerequisites

This guide assumes that you are already familiar with the concepts described in the following documents:

- [Concept: OCM controllers]({{< relref "/docs/concepts/ocm-controllers.md" >}}) - OCM Controllers
- [Installed Kro](https://kro.run/docs/getting-started/Installation/)

## When is this needed?

The controller uses [server-side apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/) to create
and manage the resources defined in your `Deployer` specs. If a `Deployer` targets a custom resource type, the
controller needs RBAC permissions for that resource's API group.

This applies to both custom resources and standard Kubernetes resources. Common examples:

- **kro** `ResourceGraphDefinitions` (`kro.run`)
- `Deployments` (`apps`) and `Services` (`core`)
- Any other resource type your deployers create

## Create a ClusterRole and ClusterRoleBinding

Create a `ClusterRole` with the permissions your deployers require, then bind it to the controller's service account.

{{<callout context="note" title="The service account name depends on the Helm release name" icon="outline/info-circle">}}
The binding below uses `ocm-k8s-toolkit-controller-manager` as the service account name only when the chart's release name contains `ocm-k8s-toolkit`. Other release names (and GitOps tools such as Flux) produce names like `<release-name>-ocm-k8s-toolkit-controller-manager`. Pin it with `fullnameOverride: ocm-k8s-toolkit`, or look it up: `kubectl get sa -n ocm-k8s-toolkit-system -l app.kubernetes.io/name=ocm-k8s-toolkit`.
{{</callout>}}

Below is an example granting permissions for kro `ResourceGraphDefinitions` and the Kubernetes resources that the deployer manages:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: ocm-controller-custom
rules:
  - apiGroups:
      - kro.run
    resources:
      - resourcegraphdefinitions
    verbs:
      - create
      - delete
      - list
      - patch
      - update
      - watch
  - apiGroups:
      - apps
    resources:
      - deployments
    verbs:
      - create
      - delete
      - get
      - list
      - patch
      - update
      - watch
  - apiGroups:
      - ""
    resources:
      - services
    verbs:
      - create
      - delete
      - get
      - list
      - patch
      - update
      - watch
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ocm-controller-custom
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: ocm-controller-custom
subjects:
  - kind: ServiceAccount
    name: ocm-k8s-toolkit-controller-manager # replace with your service account name if you did not pin it (see above)
    namespace: ocm-k8s-toolkit-system
```

Apply it to your cluster:

```bash
kubectl apply -f custom-rbac.yaml
```

{{<callout context="caution" title="Least Privilege" icon="outline/alert-triangle">}}
Follow the principle of least privilege. Only grant the verbs and resources your deployers actually need.
{{</callout>}}

## Verifying permissions

After applying, confirm that the controller has the expected access:

```bash
kubectl auth can-i create resourcegraphdefinitions.kro.run \
  --as=system:serviceaccount:ocm-k8s-toolkit-system:ocm-k8s-toolkit-controller-manager
```

The output should be `yes`.

## Multiple deployer targets

If your deployers target several custom resource types, add additional rules to the same `ClusterRole`:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: ocm-controller-custom
rules:
  - apiGroups:
      - kro.run
    resources:
      - resourcegraphdefinitions
    verbs:
      - create
      - delete
      - list
      - patch
      - update
      - watch
  - apiGroups:
      - your.custom.group
    resources:
      - yourresources
    verbs:
      - create
      - delete
      - list
      - patch
      - update
      - watch
```

## RBAC for CRDs kro creates at runtime

Everything above grants RBAC to the **OCM controller's** `ServiceAccount`. If your `Deployer` targets a kro
`ResourceGraphDefinition` (RGD), there is a second, separate RBAC concern: **kro's own** `ServiceAccount`.

An RGD can define a brand-new schema-based kind (for example, a `Podinfo` or `Bootstrap` kind). Registering
that kind's CRD is covered by kro's own installation role: in [aggregation mode](https://kro.run/docs/advanced/access-control),
the base role already includes access to `ResourceGraphDefinition`s and `CustomResourceDefinition`s; the
dev-friendly `unrestricted` mode used in the [setup guide]({{< relref "/docs/getting-started/setup-controller-environment.md" >}})
grants everything broadly. Neither mode's *base* role covers what happens next: the moment kro (or the RGD)
creates an *instance* of that new kind, or any other object the RGD manages, kro's `ServiceAccount` needs RBAC
for that specific kind, no differently than the OCM controller needs RBAC for the resources its `Deployer`
applies.

This catches people off guard because the dev-friendly kro install (as used in the [setup guide]({{< relref
"/docs/getting-started/setup-controller-environment.md" >}})) grants broad, cluster-wide permissions, so the
gap only surfaces once you move to a hardened cluster with least-privilege RBAC. [kro's access control
guide](https://kro.run/docs/advanced/access-control) covers the least-privilege setup for kro itself.

Grant kro's `ServiceAccount` a `ClusterRole` the same way as above, scoped to the kinds your RGDs create and
manage. For example, an RGD that defines a `Podinfo` kind and renders a Deployment and Service needs:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kro-custom
rules:
  - apiGroups:
      - kro.run
    resources:
      - podinfos
    verbs:
      - create
      - delete
      - get
      - list
      - patch
      - update
      - watch
  - apiGroups:
      - kro.run
    resources:
      - podinfos/status
    verbs:
      - get
      - patch
      - update
  - apiGroups:
      - delivery.ocm.software
    resources:
      - resources
    verbs:
      - get
      - list
      - watch
  - apiGroups:
      - apps
    resources:
      - deployments
    verbs:
      - create
      - delete
      - get
      - list
      - patch
      - update
      - watch
  - apiGroups:
      - ""
    resources:
      - services
    verbs:
      - create
      - delete
      - get
      - list
      - patch
      - update
      - watch
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kro-custom
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kro-custom
subjects:
  - kind: ServiceAccount
    name: kro # replace with your kro release's service account name if you did not use the default
    namespace: kro-system
```

The exact kinds depend on what your own RGDs create. See [Deploy an Application from a Helm Chart with OCM
and kro]({{< relref "/docs/tutorials/deploy-helm-chart-bootstrap.md" >}}) and [Deploy an Application from
Plain Manifests with OCM and kro]({{< relref "/docs/tutorials/deploy-plain-manifests.md" >}}) for two worked
examples of the specific kinds each pattern needs.

## Related Documentation

- [Concept: OCM controllers]({{< relref "/docs/concepts/ocm-controllers.md" >}}) - Learn how the OCM Controllers work and how they interact with deployers and Kubernetes resources.
- [kro's access control guide](https://kro.run/docs/advanced/access-control) - Least-privilege RBAC setup for kro itself.
