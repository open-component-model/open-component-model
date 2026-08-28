---
title: "Correlating metadata to OCM"
description: "The ODG data model for correlating typed metadata with OCM artefacts."
weight: 2
toc: true
---

## What is the ArtefactMetadata model?

The Open Delivery Gear's data model correlates typed metadata from multiple sources with the artefacts that metadata belongs to. At its core it is the `ArtefactMetadata` type: a structure that pairs an artefact identifier (the correlation ID) with extension-specific payload and metadata describing the payload's origin and type. `ArtefactMetadata` is the output of an extension, uploaded to the ODG database via the ODG API, and then used for further processing and reporting. The model is defined in the `odg.model` module of odg-core ([ref](https://github.com/open-component-model/odg-core/blob/master/src/odg/model.py)).

<img src="/odg/artefact-metadata.svg" alt="Fig. 1: Artefact Metadata Model">

## Why does it exist?

Compliance tools produce heterogeneous outputs — vulnerabilities, SBOMs, scan timestamps, responsible owners — against many different artefact types. A common data model lets every extension store its results in the same database, query them with a shared API, and group related findings regardless of which tool produced them. Using the artefact as a correlation ID means findings from different versions or scan runs can be compared, discovery dates preserved, and GitHub issues updated rather than recreated.

<img src="/odg/general-overview.svg" alt="Fig. 2: General Overview">

## How it works

### Artefact

The *Artefact* identifies where the [Payload](#payload) belongs to. Artefacts fall into two groups:

#### Designtime Artefacts *(e.g. OCI images, Helm charts, source code)*

Designtime artefacts are statically available right after the build. They are typically already modelled via OCM as `resources` or `sources` and translate directly into the `artefact` model of `ArtefactMetadata`. The supported `artefact_kinds` are `resource` and `source`.

```yaml
# OCM component descriptor (excerpt)
meta:
  schemaVersion: v2
component:
  name: example.org/my-component
  version: 0.1.0
  resources: # might be `sources` as well
    - name: my-image
      version: 0.1.0
      type: ociImage
      extraIdentity:
        version: 0.1.0
```

```yaml
artefact:
  component_name: example.org/my-component
  component_version: 0.1.0
  artefact_kind: resource # might be `source` as well
  artefact:
    artefact_name: my-image
    artefact_version: 0.1.0
    artefact_type: ociImage
    artefact_extra_id:
      version: 0.1.0
```

#### Runtime Artefacts *(e.g. Kubernetes clusters, hyperscaler resources)*

Runtime artefacts cannot be statically modelled via OCM because they are ephemeral and not related to the build process. They must be modelled individually. Each runtime artefact must be unambiguously identifiable and must share properties that allow related artefacts to be grouped together (e.g. `artefact_type`).

```yaml
artefact:
  component_name: example.org/my-landscape-component # OCM component name of the landscape
  component_version: 0.1.0 # current version of the landscape
  artefact_kind: runtime
  artefact:
    artefact_name: managed-seeds # group of Kubernetes clusters, might also be a project etc.
    artefact_version: diki # Diki does not specify an actual version here
    artefact_type: dikiReport # Diki does not specify multiple artefact types
```

```yaml
artefact:
  component_name: example.org/my-landscape-component # OCM component name of the landscape
  artefact_kind: runtime
  artefact:
    artefact_name: instance-abc # instance-id of a hyperscale resource
    artefact_type: aws/virtual-machine # Inventory uses different artefact types here
    artefact_extra_id:
      account_id: 0123456789
      region_name: eu-west-1
      vpc_id: vpc-0123456789
```

When defining `artefact` properties, the **correlation-id** is used to find related data or to create logical groups — for example, to group items into the same GitHub issue. The attributes used for grouping can be configured freely, but the included properties must be "stable". Including a *version* property or a temporary *instance-id* as a grouping-relevant property would prevent correlating the same payload across multiple versions or instances, causing discovery dates to be reset or new GitHub issues to be created instead of existing ones being updated.

### Metadata

The `meta` field holds information on where the payload comes from (`datasource`) and what type of payload it is (`type`). In most cases, `datasource` equals the name of the extension. Both `datasource` and `type` share a global namespace. There are three kinds of datatypes:

1. **Meta Types**

   Not directly related to any finding or single extension — used internally by ODG. Most extensions do not need to define these. The most prominent is `meta/artefact_scan_info`, which must be emitted by an extension for every processed artefact to indicate successful processing, and which contains information on the last execution (e.g. a timestamp or reference). The relationship of a meta type to an artefact is 1:1.

   *Examples:* `meta/artefact_scan_info`, `meta/responsibles`

2. **Finding Types**

   Finding types describe deviations from a desired state defined by a ruleset — for example the presence of a known vulnerability. Findings can be assigned a severity, and because they must be resolved within a certain timeframe, `ArtefactMetadata` entries for findings must provide an initial `discovery_date` together with `allowed_processing_time`. Extensions may also add responsible information via `.meta.responsibles` to override the default fallback (see [Lifecycling GitHub Issues]({{< relref "docs/concepts/odg/lifecycling-github-issues/" >}})). The relationship of findings to an artefact is n:1.

3. **Informational Types**

   Data collected for an artefact that is not a finding should be modelled as an informational datatype. This information may enrich reported findings. For example, in the context of vulnerabilities, an informational type holds detected file paths to add package location to the report — file paths are not part of the vulnerability finding payload itself because the relationship is n:n.

To create a mapping between the `Datasource` and the `Datatypes` it emits (and vice-versa), the respective util functions `datasource()` and `datatypes()` must be updated.

### Payload

The schema of the *Payload* (`data`) can be individually defined by the extension to store the actual content. A new dataclass with the desired structure must be added for each `Datatype`, and then registered as an allowed type for the `data` property of the `ArtefactMetadata` model class. Type definitions must be consistent for each model element of the same `Datatype`.

#### Key

To unambiguously identify existing database entries, each `ArtefactMetadata` instance must define a unique `key` property. This `key` always consists of the `artefact`, `Datasource`, `Datatype`, and the `key` defined by the `data` class (if any). If multiple entries per tuple of `artefact`, `Datasource`, and `Datatype` are expected, the data class must also define a unique `key` property.

{{< callout type="note" >}}
See [gardener/cc-utils#1166](https://github.com/gardener/cc-utils/pull/1166/files)
as an example for this section. Note that the `dso.model` module in
that pull request has been replaced by the `odg.model` module in odg-core.
{{< /callout >}}

### Discovery Date

Findings typically must be processed within an allowed timeframe, so the date of first discovery is stored to allow calculation of the latest due date. The initial `discovery_date` must be retained during subsequent updates. By default, the initial `discovery_date` is reused when the OCM identity (except its version and extra identity) and the `key` property of the finding match. If a deviation from this default is desired (e.g. when the `key` contains a package version that should not be considered for reuse), a custom check must be implemented in the upload metadata route.

In the most trivial case, reuse occurs when the `data` key is equal. However, there are cases where this is not enough — for vulnerability findings, the `discovery_date` must be reused when the CVE and package are the same even if the package version (which is part of the `data` key) changes. This behaviour must be defined in the `PUT /artefacts/metadata` route (see [open-component-model/odg-core@6697e50](https://github.com/open-component-model/odg-core/commit/6697e5045d080d72c70b2ccaa214ffcaa8d0e244) as an example). If not defined, the `discovery_date` is always consumed as provided in the new `ArtefactMetadata` entry.

#### Artefact Scan Info example

```yaml
artefact:
  component_name: example.org/my-component
  component_version: 0.1.0
  artefact_kind: resource
  artefact:
    artefact_name: my-image
    artefact_version: 0.1.0
    artefact_type: ociImage
    artefact_extra_id:
      version: 0.1.0
meta:
  type: meta/artefact_scan_info
  datasource: bdba # name of the new extension
data: {} # optional properties describing the scan
```

## Key properties

| Property | Description |
| --- | --- |
| Correlation model | All data is linked to an `artefact` identifier |
| Payload schema | Individually defined per extension `Datatype` |
| Key uniqueness | Derived from artefact + datasource + datatype + data key |
| Discovery date | Retained across updates; custom logic configurable per route |
| Datasource | Global namespace shared by all extensions |
| Datatype categories | Meta, Finding, Informational |

## Relationship to other concepts

- [ODG System Architecture]({{< relref "docs/concepts/odg/odg-system-architecture/" >}}) — explains how `ArtefactMetadata` flows through the system from extensions to the ODG database
- [Reacting upon OCM events]({{< relref "docs/concepts/odg/reacting-upon-ocm-events/" >}}) — describes how the artefact enumerator creates backlog items that trigger extensions to produce `ArtefactMetadata`
- [Lifecycling GitHub Issues]({{< relref "docs/concepts/odg/lifecycling-github-issues/" >}}) — consumes finding-type `ArtefactMetadata` to create and update GitHub issues
- [Overwriting OCM component responsibles]({{< relref "docs/concepts/odg/overwriting-ocm-component-responsibles/" >}}) — uploads `meta/responsibles` `ArtefactMetadata` entries
- [SLA Violation Profiler]({{< relref "docs/concepts/odg/sla-violation-profiler/" >}}) — queries finding `ArtefactMetadata` to compute SLA compliance evidence

## When to use it

Refer to this document when:

- You are **building a new extension** and need to define its data model
- You are **debugging why discovery dates are being reset** across scans
- You are **designing artefact grouping** for GitHub issue reporting
- You need to understand **how ODG stores and queries compliance data**

## Next steps

- [Contribute a new Extension]({{< relref "docs/tutorials/odg/contribute-a-new-extension/" >}})
- [Prepare your component for ODG]({{< relref "docs/how-to/odg/prepare-your-component-for-odg/" >}})

## Related documentation

- [ODG System Architecture]({{< relref "docs/concepts/odg/odg-system-architecture/" >}})
- [Reacting upon OCM events]({{< relref "docs/concepts/odg/reacting-upon-ocm-events/" >}})
- [Lifecycling GitHub Issues]({{< relref "docs/concepts/odg/lifecycling-github-issues/" >}})
- [Overwriting OCM component responsibles]({{< relref "docs/concepts/odg/overwriting-ocm-component-responsibles/" >}})
- [Generating Software Bill of Materials]({{< relref "docs/concepts/odg/generating-software-bill-of-materials/" >}})
- [SLA Violation Profiler]({{< relref "docs/concepts/odg/sla-violation-profiler/" >}})
