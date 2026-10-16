---
title: Configure Custom RBAC for Deployers
description: "How to grant the OCM controller and your NamespacedDeployer service accounts the RBAC permissions required by your deployer targets (e.g. kro ResourceGraphDefinitions)."
weight: 115
toc: true
---

The OCM Kubernetes Controller Toolkit ships with the minimum RBAC permissions needed to manage its own custom resources
(`Repository`, `Component`, `Resource`, `Deployer`, `NamespacedDeployer`). It does **not** include permissions for
the objects your deployers create, such as kro `ResourceGraphDefinitions`. You grant those yourself, and who needs
which verbs depends on the deployer kind:

| Deployer kind        | Write verbs (`create`, `update`, `patch`, `delete`)  | Read verbs (`get`, `list`, `watch`) |
|----------------------|------------------------------------------------------|-------------------------------------|
| `NamespacedDeployer` | Its service account                                  | The controller's service account    |
| `Deployer`           | The controller's service account                     | The controller's service account    |

## Prerequisites

This guide assumes that you are already familiar with the concepts described in the following documents:

- [Concept: OCM controllers]({{< relref "/docs/concepts/ocm-controllers.md" >}}) - OCM Controllers
- [Concept: Kubernetes Deployer]({{< relref "/docs/concepts/kubernetes-deployer.md" >}}) - `Deployer` and `NamespacedDeployer`
- [kro installed](https://kro.run/docs/getting-started/Installation/)

## When is this needed?

The deployers use [server-side apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/) to create
and manage the objects in the deployed resource, and the controller watches those objects for drift. Every kind a
deployer creates needs RBAC, custom resources and standard Kubernetes resources alike. Common examples:

- **kro** `ResourceGraphDefinitions` (`kro.run`)
- `Deployments` (`apps`) and `Services` (`core`)
- Any other resource type your deployers create

## NamespacedDeployer: grant the service account

A `NamespacedDeployer` does not apply with the controller's permissions. It impersonates the service account in
`spec.serviceAccountName`, so write access goes to it through a `Role` and `RoleBinding` in the deployer's
namespace. Without `spec.serviceAccountName` it impersonates the `default` service account of its namespace, which
you grant in the same way. Prefer a dedicated service account, as every pod in the namespace that names none runs as
`default`.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: my-deployer
  namespace: my-team
rules:
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: my-deployer
  namespace: my-team
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: my-deployer
subjects:
  - kind: ServiceAccount
    name: my-deployer
    namespace: my-team
```

Bind the service account by name. The controller impersonates only the user name, so bindings to groups such as
`system:serviceaccounts:my-team` do not apply.

For objects in another namespace, create the `Role` and `RoleBinding` in that namespace instead. The subject keeps
the service account's own namespace (`my-team`), and the deployer gets access to `shared-services` only:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: my-deployer
  namespace: shared-services
rules:
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: my-deployer
  namespace: shared-services
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: my-deployer
subjects:
  - kind: ServiceAccount
    name: my-deployer
    namespace: my-team
```

Only cluster-scoped kinds such as kro `ResourceGraphDefinitions` need a `ClusterRole` with a `ClusterRoleBinding`.
Grant only the kinds the deployment needs.

Keep these permissions until the `NamespacedDeployer` is deleted. It prunes with them. Without them its deletion
waits with a `DeletionFailed` condition until you restore them, and without the service account itself it orphans
cluster-scoped and cross-namespace objects.

Check the service account's access:

```bash
kubectl auth can-i create deployments.apps -n my-team \
  --as=system:serviceaccount:my-team:my-deployer
```

The controller additionally needs read access to the same kinds, see the next section.

## Grant the controller read access

The controller watches deployed objects for drift with its own identity, for both deployer kinds. Create a
`ClusterRole` with `get`, `list` and `watch` on every kind your deployers deploy, then bind it to the controller's
service account.

{{<callout context="note" title="The service account name depends on the Helm release name" icon="outline/info-circle">}}
The binding below uses `ocm-k8s-toolkit-controller-manager` as the service account name only when the chart's release name contains `ocm-k8s-toolkit`. Other release names (and GitOps tools such as Flux) produce names like `<release-name>-ocm-k8s-toolkit-controller-manager`. Pin it with `fullnameOverride: ocm-k8s-toolkit`, or look it up: `kubectl get sa -n ocm-k8s-toolkit-system -l app.kubernetes.io/name=ocm-k8s-toolkit`.
{{</callout>}}

Below is an example for kro `ResourceGraphDefinitions` and the Kubernetes resources that the deployer manages:

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
      - get
      - list
      - watch
  - apiGroups:
      - apps
    resources:
      - deployments
    verbs:
      - get
      - list
      - watch
  - apiGroups:
      - ""
    resources:
      - services
    verbs:
      - get
      - list
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

Confirm that the controller has the expected access:

```bash
kubectl auth can-i watch resourcegraphdefinitions.kro.run \
  --as=system:serviceaccount:ocm-k8s-toolkit-system:ocm-k8s-toolkit-controller-manager
```

The output should be `yes`.

## Deployer: grant the controller write access

A cluster-scoped `Deployer` applies and prunes with the controller's own permissions. Only if you still run one, add
the write verbs to the rules in `ocm-controller-custom` for the kinds it deploys:

```yaml
  - apiGroups:
      - kro.run
    resources:
      - resourcegraphdefinitions
    verbs:
      - create
      - delete
      - get
      - list
      - patch
      - update
      - watch
```

Confirm it with `kubectl auth can-i create` instead of `watch` in the check above.

These verbs let every `Deployer` write those kinds anywhere in the cluster. After you
[migrate to the NamespacedDeployer]({{< relref "migrate-to-namespaced-deployer.md" >}}), remove them again and keep
only the read verbs.

{{<callout context="caution" title="Least Privilege" icon="outline/alert-triangle">}}
Follow the principle of least privilege. Only grant the verbs and resources your deployers actually need.
{{</callout>}}

## Multiple deployer targets

If your deployers target several resource types, add a rule per type to the same `ClusterRole`:

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
      - get
      - list
      - watch
  - apiGroups:
      - your.custom.group
    resources:
      - yourresources
    verbs:
      - get
      - list
      - watch
```

Grant the same kinds with write verbs to the `NamespacedDeployer`'s service account.

## RBAC for CRDs kro creates at runtime

Everything above grants RBAC to the **OCM controller's** and your **deployer's** `ServiceAccount`. If your deployer
targets a kro `ResourceGraphDefinition` (RGD), there is a second, separate RBAC concern: **kro's own**
`ServiceAccount`.

An RGD can define a brand-new schema-based kind (for example, a `Podinfo` or `Bootstrap` kind). In kro's
least-privilege [aggregation mode](https://kro.run/docs/advanced/access-control), creating and managing
instances of that kind, or any other object the RGD templates, requires extra RBAC for kro's own
`ServiceAccount`. The [setup guide]({{< relref "/docs/getting-started/setup-controller-environment.md" >}})
installs kro in this mode and already creates such roles for the OCM resources and for the Flux or ArgoCD
resources.

Create a `ClusterRole` scoped to the kinds your RGDs create and manage, and label it
`rbac.kro.run/aggregate-to-controller: "true"`. In aggregation mode kro picks up every `ClusterRole` with this
label, so you do not need a binding. See [kro's access control guide](https://kro.run/docs/advanced/access-control).
For example, an RGD that defines a `Podinfo` kind and renders a Deployment and Service needs:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kro-custom
  labels:
    # kro picks up every ClusterRole with this label, see https://kro.run/docs/advanced/access-control
    rbac.kro.run/aggregate-to-controller: "true"
rules:
  - apiGroups:
      - kro.run
    resources:
      - podinfoes
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
      - podinfoes/status
    verbs:
      - get
      - patch
      - update
  - apiGroups:
      - delivery.ocm.software
    resources:
      - resources
    verbs:
      - create
      - delete
      - get
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
```

The exact kinds depend on what your own RGDs create. Grant the full set of verbs (not just `get`/`list`/`watch`)
for every kind that appears as a `template:` in an RGD's `resources:` list, even one owned by a different
controller entirely, like the `delivery.ocm.software` `Resource` above: kro creates and deletes it as part of
the resource graph the same way it does for a `Deployment` or `Service`, so read-only access isn't enough,
regardless of which API group the kind belongs to.

See [Deploy an Application from a Helm Chart with OCM
and kro]({{< relref "/docs/tutorials/deploy-helm-chart-bootstrap.md" >}}) and [Deploy an Application from
Chained RGDs with OCM and kro]({{< relref "/docs/tutorials/deploy-chained-rgds.md" >}}) for two worked
examples of the specific kinds each pattern needs.

## Related Documentation

- [Concept: OCM controllers]({{< relref "/docs/concepts/ocm-controllers.md" >}}) - Learn how the OCM Controllers work and how they interact with deployers and Kubernetes resources.
- [kro's access control guide](https://kro.run/docs/advanced/access-control) - Least-privilege RBAC setup for kro itself.
