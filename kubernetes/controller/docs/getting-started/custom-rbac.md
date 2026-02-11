---
title: Configuring Custom RBAC for Deployers
description: "How to grant the OCM K8s Toolkit controller additional RBAC permissions required by your deployer targets (e.g. kro ResourceGraphDefinitions)."
icon: ":lock:"
weight:
toc: true
---

# Configuring Custom RBAC for Deployers

The OCM K8s Toolkit controller, ships with the minimum RBAC permissions needed to manage its own custom resources
(`Repository`, `Component`, `Resource`, `Deployer`). It does **not** include permissions for third-party resources
that your deployers may create or manage.

If your `Deployer` resources produce custom resources (e.g. kro `ResourceGraphDefinitions`), you must grant the
controller's service account the necessary permissions yourself.

## When is this needed?

The controller uses [server-side apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/) to create
and manage the resources defined in your `Deployer` specs. If a `Deployer` targets a custom resource type, the
controller needs RBAC permissions for that resource's API group.

This applies to both custom resources and standard Kubernetes resources. Common examples:

- **kro** `ResourceGraphDefinitions` (`kro.run`)
- `Deployments` (`apps`) and `Services` (core)
- Any other resource type your deployers create

## Create a ClusterRole and ClusterRoleBinding

Create a `ClusterRole` with the permissions your deployers require, then bind it to the controller's service account.

Below is an example granting permissions for kro `ResourceGraphDefinitions` and the Kubernetes resources
that the ApplySet deployer manages:

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
    name: ocm-k8s-toolkit-controller-manager
    namespace: ocm-k8s-toolkit-system
```

Apply it to your cluster:

```bash
kubectl apply -f custom-rbac.yaml
```

> [!IMPORTANT]
> Follow the principle of least privilege. Only grant the verbs and resources your deployers actually need.

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
