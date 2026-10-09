---
title: "Migrate a Deployer to a NamespacedDeployer"
description: "Hand the objects of a cluster-scoped Deployer over to a NamespacedDeployer without deleting or recreating them."
weight: 39
toc: true
---

## Goal

Replace a cluster-scoped `Deployer` with a `NamespacedDeployer` while the deployed objects keep running.

## You'll end up with

- A `NamespacedDeployer` that owns and reconciles the objects the `Deployer` deployed
- The same objects, with unchanged UIDs, no longer owned by the old `Deployer`

**Estimated time:** ~10 minutes

## Prerequisites

- OCM Controllers with the `NamespacedDeployer` controller enabled (Helm value `manager.namespacedDeployer.enabled`, on by default)
- The `NamespacedDeployer` lives in the namespace of the `Deployer`'s `Resource`. Its OCM configuration references
  must point to that namespace as well, see [Kubernetes Deployer]({{< relref "docs/concepts/kubernetes-deployer.md" >}}).

{{< callout type="warning" >}}
⚠️ **Never delete an active `Deployer` before the hand-over.** Its finalizer prunes everything it deployed.
{{< /callout >}}

The examples use a `Deployer` named `my-deployer` that references the `Resource` `my-resource` in namespace `my-team`.

## Steps

### Create the service account

The `NamespacedDeployer` applies with a service account. Grant it the kinds the `Deployer` deploys, as shown in
[Configure Custom RBAC for Deployers]({{< relref "custom-rbac.md" >}}). Use a `ClusterRole` and
`ClusterRoleBinding` for cluster-scoped kinds such as kro `ResourceGraphDefinitions` or for objects in other
namespaces:

```bash
kubectl create serviceaccount my-deployer -n my-team
kubectl create role my-deployer -n my-team \
  --verb=get,list,watch,create,update,patch,delete --resource=deployments.apps,services
kubectl create rolebinding my-deployer -n my-team --role=my-deployer --serviceaccount=my-team:my-deployer
```

### Suspend the Deployer

Stop the `Deployer` from reconciling, so it does not re-apply its ownership while the `NamespacedDeployer` takes over:

```bash
kubectl patch deployer.delivery.ocm.software my-deployer --type merge -p '{"spec":{"suspend":true}}'
```

### Release the objects from the Deployer's ApplySet

Every deployed object carries the `applyset.kubernetes.io/part-of` label of the `Deployer`'s ApplySet, and a
deployer refuses to take over objects of another ApplySet. Look up the ApplySet ID and the kinds it contains:

```bash
kubectl get deployer.delivery.ocm.software my-deployer \
  -o jsonpath='{.metadata.labels.applyset\.kubernetes\.io/id}{"\n"}{.metadata.annotations.applyset\.kubernetes\.io/contains-group-kinds}{"\n"}'
```

You should see the ID and the kinds, for example `applyset-uw2P...-v1` and `Deployment.apps,Service`.
Remove the label from **every** kind in that list. `--all-namespaces` also covers objects the `Deployer` placed in
other namespaces, and cluster-scoped kinds such as `ResourceGraphDefinition.kro.run` work the same way:

```bash
export APPLYSET_ID=<id from above>
kubectl label deployments.apps,services --all-namespaces \
  -l applyset.kubernetes.io/part-of=$APPLYSET_ID applyset.kubernetes.io/part-of-
```

Replace `deployments.apps,services` with the kinds from your list. Afterwards this should print nothing for each
kind:

```bash
kubectl get deployments.apps,services --all-namespaces -l applyset.kubernetes.io/part-of=$APPLYSET_ID
```

### Create the NamespacedDeployer

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: NamespacedDeployer
metadata:
  name: my-deployer
  namespace: my-team
spec:
  resourceRef:
    name: my-resource
  serviceAccountName: my-deployer
```

Apply it and wait until it is ready:

```bash
kubectl apply -f namespaced-deployer.yaml
kubectl wait namespaceddeployer.delivery.ocm.software/my-deployer -n my-team --for=condition=Ready --timeout=5m
```

Both deployers apply with the same server-side apply field manager, so the `NamespacedDeployer` adopts the released
objects and replaces their owner reference. Check one of them:

```bash
kubectl get deployment <name> -n my-team -o jsonpath='{.metadata.ownerReferences[*].kind}'
```

You should see: `NamespacedDeployer`.

### Remove the Deployer

Pick one of the two ways. Both leave the objects in place.

{{< tabs "remove-deployer" >}}
{{< tab "Delete, then unsuspend" >}}
Mark the `Deployer` for deletion, then unsuspend it. It runs its finalizers before it would apply again, and its
prune finds nothing because no object carries its ApplySet ID anymore.

```bash
kubectl delete deployer.delivery.ocm.software my-deployer --wait=false
kubectl patch deployer.delivery.ocm.software my-deployer --type merge -p '{"spec":{"suspend":false}}'
```
{{< /tab >}}
{{< tab "Orphan" >}}
Remove the finalizers so no cleanup runs, then delete the `Deployer` without cascading to its dependents.

```bash
kubectl patch deployer.delivery.ocm.software my-deployer --type json \
  -p '[{"op":"remove","path":"/metadata/finalizers"}]'
kubectl delete deployer.delivery.ocm.software my-deployer --cascade=orphan
```
{{< /tab >}}
{{< /tabs >}}

### Verify the hand-over

```bash
kubectl get deployer.delivery.ocm.software my-deployer
kubectl get namespaceddeployer.delivery.ocm.software my-deployer -n my-team
```

You should see: `NotFound` for the `Deployer` and `Ready` for the `NamespacedDeployer`. The deployed objects keep
their UIDs.

## Troubleshooting

### Symptom: `resource belongs to a different ApplySet`

**Cause:** An object still carries the `Deployer`'s `applyset.kubernetes.io/part-of` label.

**Fix:** Repeat the release step for the kind named in the message.

### Symptom: the NamespacedDeployer reports `forbidden`

**Cause:** The service account lacks a verb, kind or namespace that the manifest needs.

**Fix:** Add the missing rule to the service account's `Role` or `ClusterRole`. The message names the kind and verb.

### Symptom: `is outside of namespace`

**Cause:** An OCM configuration reference points to another namespace.

**Fix:** Copy the referenced `Secret` or `ConfigMap` into the `NamespacedDeployer`'s namespace and reference it there.

### Symptom: the Deployer stays in `Terminating`

**Cause:** It is still suspended, so its finalizers never run.

**Fix:** Unsuspend it, or remove its finalizers as in the orphan tab.

## Related documentation

- [Concept: Kubernetes Deployer]({{< relref "docs/concepts/kubernetes-deployer.md" >}})
- [How-to: Configure Custom RBAC for Deployers]({{< relref "custom-rbac.md" >}})
