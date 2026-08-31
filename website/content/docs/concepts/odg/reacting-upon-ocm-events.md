---
title: "Reacting upon OCM events"
description: "How the artefact-enumerator extension manages compliance snapshots and backlog items."
weight: 3
toc: true
---

## What is the artefact enumerator?

The artefact-enumerator is an ODG extension that periodically checks configured OCM components and runtime artefacts, manages the lifecycle of their compliance snapshots (create/update/delete), and triggers other extensions by creating backlog items. It is the primary mechanism through which ODG reacts to changes in the software landscape — new component versions, removed artefacts, or elapsed scan intervals — without requiring manual intervention.

## Why does it exist?

Compliance scanning must be continuous and automatic. Without a component that tracks which artefacts need processing and when, each extension would have to independently discover components, track state, and coordinate with others. The artefact enumerator centralises this orchestration: it maintains a shared record of what exists, what has been processed, and what still needs work, so extensions only need to consume backlog items and upload results.

## How it works

### Artefacts

A set of **artefacts "of interest"** must be specified for periodic processing by available extensions (e.g. scanning, reporting). Apart from periodic triggers initiated by the artefact-enumerator, certain extensions can also be triggered manually by creating a **backlog item** for the desired extension and artefact (see [Backlog Item example](#backlog-item-example)) — for example via the delivery-dashboard, the ODG API, or the cluster API. In general, creating backlog items is the same trigger as used by the artefact-enumerator, but certain extensions may require artefacts to be configured here to process them (e.g. [Lifecycling GitHub Issues]({{< relref "docs/concepts/odg/lifecycling-github-issues/" >}})).

Artefacts may be configured in two ways:

#### OCM Components

OCM components are configured using the `components` property in the extensions configuration. The configured components and their dependencies are retrieved **recursively** and each dependency is subject to processing. ODG works at the granularity of `ComponentArtefactIds` — each `resource` and `source` of the OCM components is parsed into such a `ComponentArtefactId` and tracked individually.

```yaml
artefact_enumerator:
  components:
    - component_name: example.org/my-component
      ocm_repo_url: europe-docker.pkg.dev/gardener-project/releases
      version: greatest
      max_versions_limit: 1
```

#### Runtime Artefacts

To process artefacts that are not (yet) modelled via OCM — i.e. volatile **runtime artefacts** — those can be added to the list of artefacts "of interest" by creating `RuntimeArtefact` custom resources, either via ODG API or via cluster API. Because these artefacts are not modelled via OCM, the artefact-enumerator cannot resolve any dependencies; each artefact must therefore be specified via a dedicated runtime artefact. Runtime artefacts also contain a `ComponentArtefactId` and are processed equally to OCM resources and sources.

```yaml
apiVersion: delivery-gear.gardener.cloud/v1
kind: RuntimeArtefact
metadata:
  name: runtime-artefact-abcde
  namespace: delivery
spec:
  artefact:
    component_name: example.org/my-component
    component_version: 0.1.0
    artefact_kind: runtime
    artefact:
      artefact_name: my-runtime-resource
      artefact_version: 0.1.0
      artefact_type: virtual-machine
      artefact_extra_id:
        version: 0.1.0
        hyperscaler: my-hyperscaler
  creation_date: '2025-01-01T12:00:00.000000+00:00'
```

### Compliance Snapshots

Compliance snapshots are the internal state record for configured artefacts. They store information such as the last execution time by an extension, and keep track of artefacts that were previously of interest but no longer are — so that, for example, remaining open GitHub issues can be closed.

For each artefact, a compliance snapshot is created. Compliance snapshots of artefacts that are no longer "of interest" are kept for an extra grace period to allow other extensions (e.g. the [issue-replicator]({{< relref "docs/concepts/odg/lifecycling-github-issues/" >}})) to react to those changes (e.g. to close related GitHub issues).

### Backlog Item example

Extensions can be triggered manually by creating a `BacklogItem` CR directly:

```yaml
apiVersion: delivery-gear.gardener.cloud/v1
kind: BacklogItem
metadata:
  name: issuereplicator-8-abcde
  namespace: delivery
  labels:
    delivery-gear.gardener.cloud/service: issueReplicator
spec:
  artefact:
    component_name: example.org/my-component
    component_version: 0.1.0
    artefact_kind: runtime
    artefact:
      artefact_name: my-runtime-resource
      artefact_version: 0.1.0
      artefact_type: virtual-machine
      artefact_extra_id:
        version: 0.1.0
        hyperscaler: my-hyperscaler
  priority: 8
  timestamp: '2025-01-01T12:00:00.000000+00:00'
```

## Key properties

| Property | Description |
| --- | --- |
| Trigger mechanism | Kubernetes CronJob on a configurable schedule |
| Artefact resolution | OCM components resolved recursively; runtime artefacts registered individually |
| State storage | Compliance snapshots in ODG database, one per artefact |
| Grace period | Removed artefacts' snapshots are retained temporarily for downstream extensions |
| Manual trigger | Supported via BacklogItem CRDs through ODG API or cluster API |
| Granularity | Per `ComponentArtefactId` (each OCM resource and source tracked individually) |

## Relationship to other concepts

- [ODG System Architecture]({{< relref "docs/concepts/odg/odg-system-architecture/" >}}) — describes how the artefact enumerator fits into the overall component topology
- [Correlating metadata to OCM]({{< relref "docs/concepts/odg/correlating-metadata-to-ocm/" >}}) — defines the `ArtefactMetadata` model that extensions produce when processing backlog items
- [Lifecycling GitHub Issues]({{< relref "docs/concepts/odg/lifecycling-github-issues/" >}}) — depends on compliance snapshots to know which artefacts are active
- [Overwriting OCM component responsibles]({{< relref "docs/concepts/odg/overwriting-ocm-component-responsibles/" >}}) — triggered via the same backlog item mechanism
- [Generating Software Bill of Materials]({{< relref "docs/concepts/odg/generating-software-bill-of-materials/" >}}) — triggered by backlog items created by the artefact enumerator

## When to use it

Refer to this document when:

- You are **configuring which components ODG should scan** and need to understand the `components` configuration
- You are **adding a runtime artefact** that is not modelled via OCM
- You are **debugging why an extension is not processing** an artefact (compliance snapshot state, grace period)
- You want to **manually trigger** an extension for a specific artefact via a BacklogItem

## Next steps

- [Deploying the Open Delivery Gear locally]({{< relref "docs/how-to/odg/deploying-the-open-delivery-gear-locally/" >}})
- [Prepare your component for ODG]({{< relref "docs/how-to/odg/prepare-your-component-for-odg/" >}})
- [Contribute a new Extension]({{< relref "docs/tutorials/odg/contribute-a-new-extension/" >}})

## Related documentation

- [ODG System Architecture]({{< relref "docs/concepts/odg/odg-system-architecture/" >}})
- [Correlating metadata to OCM]({{< relref "docs/concepts/odg/correlating-metadata-to-ocm/" >}})
- [Lifecycling GitHub Issues]({{< relref "docs/concepts/odg/lifecycling-github-issues/" >}})
- [Overwriting OCM component responsibles]({{< relref "docs/concepts/odg/overwriting-ocm-component-responsibles/" >}})
- [Generating Software Bill of Materials]({{< relref "docs/concepts/odg/generating-software-bill-of-materials/" >}})
- [SLA Violation Profiler]({{< relref "docs/concepts/odg/sla-violation-profiler/" >}})
