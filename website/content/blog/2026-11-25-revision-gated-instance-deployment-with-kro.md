---
title: "Deliver Once, Promote When Ready: Controlled Rollouts on Shared Clusters with OCM and kro"
description: "One kro ResourceGraphDefinition, three environments, one cluster: OCM delivers a new component version once, and each environment moves to it only when you say so."
date: 2026-11-25T19:00:00+01:00
contributors: []
tags: ["ocm", "kro", "versioning", "rgd", "instances"]
draft: false
---

## The Concern

Consider the following cluster setup. Three namespaces isolate three environments: `dev`, `staging`, `prod`.

The application is delivered as an OCM component. The component carries one resource: a kro
`ResourceGraphDefinition` (RGD) that describes how to deploy the application.

```yaml
# component-constructor.yaml
components:
  - name: acme.org/demo/simple-rgd
    version: "${VERSION}"
    provider:
      name: acme.org
    resources:
      - name: rgd
        type: blob
        version: "${VERSION}"
        input:
          type: File/v1
          path: ./component/rgd-${VERSION}.yaml
```

The `Component` picks up every version that matches its semver constraint and applies the [bootstrap pattern]({{< relref "/docs/guides/deploy/deploy-helm-chart-with-kro-and-gitops.md" >}}).

```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Component
metadata:
  name: simple-rgd
  namespace: default
spec:
  component: acme.org/demo/simple-rgd
  repositoryRef:
    name: simple-rgd
  semver: ">=1.0.0" # follow the newest version
  interval: 10s
---
apiVersion: delivery.ocm.software/v1alpha1
kind: Resource
metadata:
  name: simple-rgd
  namespace: default
spec:
  componentRef:
    name: simple-rgd
  resource:
    byReference:
      resource:
        name: rgd
---
apiVersion: delivery.ocm.software/v1alpha1
kind: NamespacedDeployer
metadata:
  name: simple-rgd
  namespace: default
spec:
  resourceRef:
    name: simple-rgd
  serviceAccountName: simple-rgd-deployer
```

For the sake of this post, a ConfigMap is the application. This is the RGD in version 1.0.0 of the component:

```yaml
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: simple
spec:
  schema:
    apiVersion: v1alpha1
    kind: Simple
    spec:
      message: string | default="hello"
  resources:
    - id: cm
      template:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: ${schema.metadata.name}-cm
        data:
          message: ${schema.spec.message}
          componentVersion: "1.0.0"
```

Each environment owns one `Instance`, and gets one ConfigMap.

Dev:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  annotations:
    ...
  labels:
    ...
  name: app-cm
  namespace: dev
data:
  componentVersion: 1.0.0
  message: hello from dev
```

Staging:
```yaml
  ... # different namespace
data:
  componentVersion: 1.0.0
  message: hello from staging
```

Prod:
```yaml
  ... # different namespace
data:
  componentVersion: 1.0.0
  message: hello from prod
```

Checking the items, we can see that OCM delivered `1.0.0` and everything is using the right version:
```console
COMPONENT                  CONSTRAINT   DELIVERED
acme.org/demo/simple-rgd   >=1.0.0      1.0.0

ENV       PIN     RUNNING   STATE   COMPONENTVERSION  FEATURE
dev               1         ACTIVE  1.0.0
staging           1         ACTIVE  1.0.0
prod              1         ACTIVE  1.0.0
```

`RUNNING` header is the graph revision the `Instance` has in its status, and `PIN` header is an annotation we'll see in
a moment.

Now, a new version of the component is published, `2.0.0`, that contains a `shiny` new feature for the deployed application:

```console
$ VERSION=2.0.0 ocm add cv --repository <registry> --constructor component-constructor.yaml
 COMPONENT                │ VERSION │ PROVIDER
──────────────────────────┼─────────┼──────────
 acme.org/demo/simple-rgd │ 2.0.0   │ acme.org
```

Suddenly, you hear a loud chirping from your phone. PagerDuty is alerting. Something is not quite right. The
application is not available. You immediately take a look at the cluster to see what happened.

```console
COMPONENT                  CONSTRAINT   DELIVERED
acme.org/demo/simple-rgd   >=1.0.0      2.0.0

ENV       PIN     RUNNING   STATE   COMPONENTVERSION  FEATURE
dev               2         ACTIVE  2.0.0             shiny
staging           2         ACTIVE  2.0.0             shiny
prod              2         ACTIVE  2.0.0             shiny
```

Well, that's not good. ALL of the environments are on the new version! This is unexpected. The backend wasn't ready for it!

It should have rolled out on dev only.

## The Cause

OCM did what it was told. The `Component` follows `>=1.0.0`, so it picked up `2.0.0`, and the deployer applied the new
RGD.

The problem is what happens after that. An RGD, is cluster-scoped, and so is the CRD that kro creates. There is one
`simple` RGD for the whole cluster. kro reconciles every `Instance` against its _latest_ state. The new version only
changed the ConfigMap template, the schema stayed the same, and still everything that used that RGD got the new feature
whether it wanted it or not.

kro does track the history of an RGD but was missing the ability for `Instance`s to choose _which_ state of the RGD to use.
This has been addressed via [KREP-009](https://github.com/kubernetes-sigs/kro/blob/main/docs/design/proposals/versioning.md).

Setting the semver of the `Component` back to **1.0.0** together with `downgradePolicy: Allow` is the all or nothing
approach. The entire component is reverted now so everything that it applies is also reverted.

You could give each environment its own `Component` with its own constraints. This results in a stamped on the RGD! Every
update would update the same object and start to battle with each other for control over it. Renaming the RGD for each
environment individually is painstaking and cumbersome, and you lose you single source of truth.

This is the problem described in [ocm-project#629](https://github.com/open-component-model/ocm-project/issues/629).

## The Solution

[Pull Request 1512](https://github.com/kubernetes-sigs/kro/pull/1512), which implements the first segment of KREP-009, lets an `Instance` choose the revision of the
RGD it uses.

> **Note:** Everything shown in this post is alpha and off by default. Revision pinning and the rollout strategy need
> the `GraphRevisionPinning` feature gate. Multiple schema versions need `MultiVersionSchema`. Both are enabled in the
> kro Helm values under `config.featureGates`.

In the above example, previously, the only solution was to put _EVERYTHING_ back to 1.0.0. Dev, staging, prod. With revision
pinning, now, we can do this instead:

```console
kubectl annotate simple app -n "prod" kro.run/graph-revision="1" --overwrite
```

What is graph revision? A [GraphRevision](https://kro.run/api/crds/graphrevision) is a snapshot of an RGD's spec. They track revision history, and their
purpose is to be an anchor for safe rollouts.

By setting `kro.run/graph-revision="1"` on the prod `Instance`, we tell kro which revision of the RGD it should use to
render this `Instance`. This will revert only the prod app to the previous version:

```console
REVISION   READY   CONTENT   NAME
1          True    1.0.0     simple-r00001
2          True    2.0.0     simple-r00002

ENV       PIN     RUNNING   STATE   COMPONENTVERSION  FEATURE
dev               2         ACTIVE  2.0.0             shiny
staging           2         ACTIVE  2.0.0             shiny
prod      1       1         ACTIVE  1.0.0
```

PIN has been set to 1 and the prod application rolled back to component version 1.0.0. Checking on the ConfigMap, we can
see it was reverted to the previous state:

```yaml
  ...
data:
  componentVersion: 1.0.0
  message: hello from prod
```

Nothing changed on the OCM side. The `Component` still reports `2.0.0` as delivered, and dev and staging keep it.

## The New Flow

Now, we aren't done yet. The next version would move dev and staging right away again. If we want safe deployments,
we'll need to update the RGD and set a rollout strategy.

We cut a new version for the component, 2.0.1, that contains our RGD with the updated value:

```yaml
...
  spec:
    resources:
    - id: cm
      template:
        apiVersion: v1
        data:
          componentVersion: 2.0.0 # Not a typo: the graph is the same as in 2.0.0.
          feature: shiny
          message: ${schema.spec.message}
        kind: ConfigMap
        metadata:
          name: ${schema.metadata.name}-cm
    rollout: # New Field.
      strategy: Manual # Manually approve / select revisions for Instances.
    schema:
      apiVersion: v1alpha1
      group: kro.run
      kind: Simple
      scope: Namespaced
      spec:
        message: string | default="hello"
...
```

We release this new version. Rollout strategy is not part of the graph so kro does not issue a new revision for it and
so none of the environments update:

```console
COMPONENT                  CONSTRAINT   DELIVERED
acme.org/demo/simple-rgd   >=1.0.0      2.0.1

REVISION   READY   CONTENT   NAME
1          True    1.0.0     simple-r00001
2          True    2.0.0     simple-r00002

ENV       PIN     RUNNING   STATE   COMPONENTVERSION  FEATURE
dev               2         ACTIVE  2.0.0             shiny
staging           2         ACTIVE  2.0.0             shiny
prod      1       1         ACTIVE  1.0.0
```

Once this RGD is in place, updates won't be rolled out automatically.

Let's do that now. We push a new version of our component, 3.0.0, and observe the environments.

```console
$ VERSION=3.0.0 ocm add cv --repository <registry> --constructor component-constructor.yaml
 COMPONENT                │ VERSION │ PROVIDER
──────────────────────────┼─────────┼──────────
 acme.org/demo/simple-rgd │ 3.0.0   │ acme.org

COMPONENT                  CONSTRAINT   DELIVERED
acme.org/demo/simple-rgd   >=1.0.0      3.0.0

ENV       PIN     RUNNING   STATE   COMPONENTVERSION  FEATURE
dev               2         ACTIVE  2.0.0             shiny
staging           2         ACTIVE  2.0.0             shiny
prod      1       1         ACTIVE  1.0.0
```

All good. Delivered, and nobody was updated. OCM reports `3.0.0` as delivered, and every environment still runs what it ran
before. Delivery and rollout are now two separate decisions.

How do I figure out what revision `3.0.0` resulted in? The deployer labels the RGD with the component version it applied,
and kro reports the last revision it issued:

```console
$ kubectl get rgd simple -o custom-columns='RGD:.metadata.name,COMPONENTVERSION:.metadata.labels.app\.kubernetes\.io/version,LATESTREVISION:.status.lastIssuedRevision,STRATEGY:.spec.rollout.strategy'
RGD      COMPONENTVERSION   LATESTREVISION   STRATEGY
simple   3.0.0              3                Manual
```

Moving "dev" to 3 means updating the annotation again:

```console
kubectl annotate simple app -n "dev" kro.run/graph-revision="3" --overwrite

# Checking the environments
REVISION   READY   CONTENT   NAME
1          True    1.0.0     simple-r00001
2          True    2.0.0     simple-r00002
3          True    3.0.0     simple-r00003

ENV       PIN     RUNNING   STATE   COMPONENTVERSION  FEATURE
dev       3       3         ACTIVE  3.0.0             shinier
staging           2         ACTIVE  2.0.0             shiny
prod      1       1         ACTIVE  1.0.0
```

`dev` is now updated. The `FEATURE` column is telling us that the ConfigMap was updated too.

> **Note:** With the `Manual` strategy, a **new** `Instance` adopts the latest revision when it is created. If a new
> environment should start on an older revision, set the annotation when you **create** the `Instance`.

## Going from `v1alpha1` to `v1beta1`

So far, we have only seen an update of the same version `v1alpha1`. But now, it's possible to introduce multiple versions too.

An RGD will always contain all versions until retired. A version cannot be retired while objects are still stored in
it, or while an `Instance` is pinned to a revision that uses it.

RGD with multiple versions:

```yaml
  ...
  schema:
    apiVersion: v1beta1
    group: kro.run
    kind: CheckMultiVersionApp
    previousVersions:
    - apiVersion: v1alpha1
      served: true
      spec:
        value: string | default="hello"
    scope: Namespaced
    spec:
      value: string | default="hello"
  ...
```

This will result in a second GraphRevision object containing both versions:

```yaml
spec:
  revision: 2
  snapshot:
    generation: 2
    name: check-multi-version
    spec:
      resources:
      - id: cm
        template:
          apiVersion: v1
          data:
            value: ${schema.spec.value}
          kind: ConfigMap
          metadata:
            name: ${schema.metadata.name}-cm
      schema:
        apiVersion: v1beta1
        group: kro.run
        kind: CheckMultiVersionApp
        previousVersions:
        - apiVersion: v1alpha1
          served: true
          spec:
            value: string | default="hello"
        scope: Namespaced
        spec:
          value: string | default="hello"
```

## Known Limitations

At the time of this post, the pull request merged into kro is the first half of this change set.
It does not contain the following two functionalities from the KREP:

### Dealing with breaking changes

Right now, if an update contains a breaking change compared to the original, it is rejected. For example, a new version
removes a field, or adds a new `required`, or changes field type, etc.

### External Rollout Strategy

The KREP describes an `External` rollout strategy in which an external controller decides how to roll out. This has not
been implemented yet. Only `Manual` and `Latest` is supported.

### The Revision Pin Does Not Equal Component Version

If you pin the revision that will get you the right RGD Spec, yes. But if your component object is shared and does not
also contain the version, regardless of the pin, `Resource` objects referencing a `Component` will use the version
inside the referenced `Component` object. Make the `Component` part of the graph instead as the
[getting started guide]({{< relref "docs/getting-started/deploy-helm-chart.md" >}}) already does.

```yaml
# RGD shipped in component version 2.0.0
resources:
  - id: component
    template:
      apiVersion: delivery.ocm.software/v1alpha1
      kind: Component
      metadata:
        name: ${schema.metadata.name}-component
      spec:
        repositoryRef:
          name: ${repository.metadata.name} # a Repository, templated the same way
        component: acme.org/demo/simple-rgd
        semver: 2.0.0 # the version this RGD was shipped with
        downgradePolicy: Allow # going back to an older revision is a downgrade
        interval: 1m
  - id: resourceChart
    template:
      apiVersion: delivery.ocm.software/v1alpha1
      kind: Resource
      metadata:
        name: ${schema.metadata.name}-chart
      spec:
        componentRef:
          name: ${component.metadata.name}
        resource:
          byReference:
            resource:
              name: chart
```

## Conclusion and Further Reading

- Read the design for versioning in kro [KREP-009](https://github.com/kubernetes-sigs/kro/blob/main/docs/design/proposals/versioning.md)
- Read the pull request that this blog is based on [kro #1512](https://github.com/kubernetes-sigs/kro/pull/1512)
- Read the issue that this blog post and the versioning strategy is solving [open-component-model #629](https://github.com/open-component-model/ocm-project/issues/629)
