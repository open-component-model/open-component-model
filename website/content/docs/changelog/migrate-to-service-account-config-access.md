---
title: "Grant Service Accounts Access to Cross-Namespace OCM Configuration"
description: "Give the service accounts of OCM Controller objects read access to the Secrets, ConfigMaps and OCM objects their ocmConfig uses from other namespaces."
icon: "arrow-autofit-right"
weight: 80
toc: true
---

## Goal

Grant the service accounts of your OCM Controller objects read access to the `Secrets`, `ConfigMaps` and OCM objects
that their `ocmConfig` uses from another namespace.

{{< callout context="caution" title="Breaking change" icon="outline/alert-triangle" >}}
The controller used to read any `Secret`, `ConfigMap` or OCM object an `ocmConfig` referenced, in any namespace, with
its own permissions. It now reads a reference into another namespace only if the object's service account may `get`
it, see [Configuration Access]({{< relref "docs/concepts/ocm-controllers.md#configuration-access" >}}). An object
without that grant is no longer ready and reports `GetConfigurationFailed`.

Configuration in the object's own namespace is not affected. If all your `ocmConfig` references stay within one
namespace, you have nothing to do.
{{< /callout >}}

## You'll end up with

- A list of the objects that read configuration from another namespace
- A service account per namespace that may `get` that configuration
- Objects that are still ready after the upgrade

**Estimated time:** ~5 minutes per namespace

## Prerequisites

- [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) with permission to create `Roles` and `RoleBindings`
- [jq](https://jqlang.org/)
- The OCM Controllers chart of this release. It grants the controller `create` on `subjectaccessreviews`.

The examples use an object in namespace `my-team` that reads the `Secret` `registry-credentials` in namespace
`shared-credentials`.

## Steps

### Find the affected objects

Run this before the upgrade. It lists every object whose own or effective configuration points to another namespace:

```bash
kubectl get repositories,components,resources,namespaceddeployers,discoveries,replications -A -o json \
  | jq -r '.items[] | . as $o | (.spec.ocmConfig[]?, .status.effectiveOCMConfig[]?)
      | select((.namespace // $o.metadata.namespace) != $o.metadata.namespace)
      | "\($o.kind) \($o.metadata.namespace)/\($o.metadata.name) reads \(.kind) \(.namespace)/\(.name)"' \
  | sort -u
```

```text
Component my-team/my-component reads Secret shared-credentials/registry-credentials
```

No output means no object is affected. Inherited configuration counts as well. A `Component` that inherits the
`ocmConfig` of its `Repository` needs `get` on the `Repository`'s `Secret`.

After the upgrade, an affected object without a grant is not ready with reason `GetConfigurationFailed`. Its
condition message names the service account and every object it may not read:

```text
secrets "registry-credentials" is forbidden: service account my-team/default may not get it in namespace shared-credentials
```

### Choose the service account

Without `spec.serviceAccountName`, the controller checks the `default` service account of the object's namespace.
Every pod in the namespace that names no service account runs as `default`, so prefer a dedicated one:

```bash
kubectl create serviceaccount ocm-reader -n my-team
```

Set it on every listed object, except `NamespacedDeployer` objects. A `NamespacedDeployer` keeps its existing service
account. Grant that account `get` on the configuration it reads from other namespaces:

```yaml
spec:
  serviceAccountName: ocm-reader
```

### Grant read access

Bind a `Role` with `get` on exactly the referenced objects, in the namespace they live in:

```bash
kubectl create role ocm-config-reader -n shared-credentials \
  --verb=get --resource=secrets --resource-name=registry-credentials
kubectl create rolebinding ocm-config-reader -n shared-credentials \
  --role=ocm-config-reader --serviceaccount=my-team:ocm-reader
```

References to `Repository`, `Component` or `Resource` objects need `get` on `repositories.delivery.ocm.software`,
`components.delivery.ocm.software` or `resources.delivery.ocm.software`.

A `NamespacedDeployer`'s service account also needs `list` and `watch` on every kind it deploys, see
[Configure Custom RBAC]({{< relref "docs/guides/deploy/configure-custom-rbac.md" >}}).

### Verify

```bash
kubectl auth can-i get secret/registry-credentials -n shared-credentials \
  --as=system:serviceaccount:my-team:ocm-reader
```

You should see: `yes`.

The controller does not watch RBAC. An object that was not ready becomes ready on its next retry, at most 5 minutes
later. Grant access before the upgrade to avoid the wait.

## Troubleshooting

### Symptom: `failed to get service account`

**Cause:** The object reads configuration from another namespace, and `spec.serviceAccountName` names a service
account that does not exist in the object's namespace.

**Fix:** Create the service account or fix the name.

### Symptom: objects in one namespace report `GetConfigurationFailed` for their own namespace

**Cause:** The controller runs with `manager.configAccess.reviewSameNamespace`, so it reviews configuration in the
object's own namespace as well.

**Fix:** Grant the service account `get` on that configuration too, or turn the value off, see
[Review the Same Namespace]({{< relref "docs/concepts/ocm-controllers.md#review-the-same-namespace" >}}).

### Symptom: the `Deployer` still reads configuration from other namespaces

**Cause:** The cluster-scoped `Deployer` has no service account, so its configuration is not checked.

**Fix:** [Migrate it to a `NamespacedDeployer`]({{< relref "migrate-to-namespaced-deployer.md" >}}).

## Related documentation

- [Concept: OCM Controllers]({{< relref "docs/concepts/ocm-controllers.md" >}})
- [Guides: Configure Controller Credentials]({{< relref "docs/guides/deploy/configure-controller-credentials.md" >}})
- [Guides: Configure Custom RBAC]({{< relref "docs/guides/deploy/configure-custom-rbac.md" >}})
