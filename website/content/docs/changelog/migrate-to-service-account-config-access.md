---
title: "Grant Service Accounts Access to OCM Configuration"
description: "Keep OCM Controller objects ready after the controller started to check their service account before reading Secrets, ConfigMaps and OCM objects."
icon: "arrow-autofit-right"
weight: 80
toc: true
---

## Goal

Keep existing OCM Controller objects with `ocmConfig` ready after the upgrade. The controller now reads OCM
configuration only if the object's service account may `get` it, see
[Configuration Access]({{< relref "docs/concepts/ocm-controllers.md#configuration-access" >}}).

## You'll end up with

- A service account per namespace, or the `default` one, that may `get` the configuration its objects reference
- Ready `Repository`, `Component`, `Resource`, `NamespacedDeployer`, `Discovery` and `Replication` objects

**Estimated time:** ~5 minutes per namespace

## Prerequisites

- [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) with permission to create `Roles` and `RoleBindings`
- The OCM Controllers chart of this release. It grants the controller `create` on `subjectaccessreviews`.

{{< callout context="caution" title="Breaking change" icon="outline/alert-triangle" >}}
Before this release, the controller read any `Secret`, `ConfigMap` or OCM object an `ocmConfig` referenced, in any
namespace, with its own permissions. Objects whose service account lacks access now report `GetConfigurationFailed`.
{{< /callout >}}

The examples use namespace `my-team` and the `Secret` `registry-credentials`.

## Steps

### Find the affected objects

List the objects that are not ready because of their configuration:

```bash
kubectl get repositories,components,resources,namespaceddeployers,discoveries,replications -A -o json \
  | jq -r '.items[] | select(any(.status.conditions[]?; .reason == "GetConfigurationFailed"))
      | "\(.kind) \(.metadata.namespace)/\(.metadata.name)"'
```

The condition message names the service account and every object it may not read:

```text
secrets "registry-credentials" is forbidden: service account my-team/default may not get it in namespace my-team
```

Inherited configuration counts as well. A `Component` that inherits the `ocmConfig` of its `Repository` needs `get`
on the `Repository`'s `Secret`.

### Choose the service account

Without `spec.serviceAccountName`, the controller checks the `default` service account of the object's namespace.
Every pod in the namespace that names no service account runs as `default`, so prefer a dedicated one:

```bash
kubectl create serviceaccount ocm-reader -n my-team
```

Set it on every object in the namespace that reads configuration, except `NamespacedDeployer` objects.
A `NamespacedDeployer` keeps its existing service account. Grant that account `get` on the configuration objects it reads.:

```yaml
spec:
  serviceAccountName: ocm-reader
```

### Grant read access

Bind a `Role` with `get` on exactly the referenced objects, in the namespace they live in:

```bash
kubectl create role ocm-config-reader -n my-team \
  --verb=get --resource=secrets --resource-name=registry-credentials
kubectl create rolebinding ocm-config-reader -n my-team \
  --role=ocm-config-reader --serviceaccount=my-team:ocm-reader
```

For a reference into another namespace, create the `Role` and `RoleBinding` there and keep the subject
`my-team:ocm-reader`. References to `Repository`, `Component` or `Resource` objects need `get` on
`repositories.delivery.ocm.software`, `components.delivery.ocm.software` or `resources.delivery.ocm.software`.

A `NamespacedDeployer`'s service account also needs `list` and `watch` on every kind it deploys, see
[Configure Custom RBAC]({{< relref "docs/guides/deploy/configure-custom-rbac.md" >}}).

### Verify

```bash
kubectl auth can-i get secret/registry-credentials -n my-team --as=system:serviceaccount:my-team:ocm-reader
```

You should see: `yes`.

The controller does not watch RBAC. The objects become ready on their next retry, at most 5 minutes later.

## Troubleshooting

### Symptom: `failed to get service account`

**Cause:** `spec.serviceAccountName` names a service account that does not exist in the object's namespace.

**Fix:** Create the service account or fix the name.

### Symptom: the `Deployer` still reads configuration from other namespaces

**Cause:** The cluster-scoped `Deployer` has no service account, so its configuration is not checked.

**Fix:** [Migrate it to a `NamespacedDeployer`]({{< relref "migrate-to-namespaced-deployer.md" >}}).

## Related documentation

- [Concept: OCM Controllers]({{< relref "docs/concepts/ocm-controllers.md" >}})
- [Guides: Configure Controller Credentials]({{< relref "docs/guides/deploy/configure-controller-credentials.md" >}})
- [Guides: Configure Custom RBAC]({{< relref "docs/guides/deploy/configure-custom-rbac.md" >}})
