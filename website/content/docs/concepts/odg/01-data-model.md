---
title: "Correlating metadata to OCM"
description: "The ODG data model for correlating typed metadata with OCM artefacts."
weight: 2
toc: true
---

The data model of the Open Delivery Gear intends to correlate typed metadata
from multiple sources with [Artefacts](#artefact) said metadata is related
to. Artefacts can either be OCM Artefacts (i.e.
[Designtime Artefacts](#designtime-artefacts)), or
[Runtime Artefacts](#runtime-artefacts). They are referenced using OCM
coordinates with optional extensions.

At its core, the Open Delivery Gear's data model consists of the
`ArtefactMetadata` meta-type, which allows describing such metadata, and
correlating it to an [artefact](#artefact). It is the output of an extension which is
uploaded to the ODG database via the ODG API, and then may be
used for further processing and reporting. In the most basic form, it consists
of an [artefact](#artefact), some [metadata](#metadata) and an extension specific
[payload](#payload) (see Fig. 1). The model is defined in the `odg.model` module of
the odg-core ([ref](https://github.com/open-component-model/odg-core/blob/master/src/odg/model.py)).

<img src="/odg/artefact-metadata.svg" alt="Fig. 1: Artefact Metadata Model">

The [artefact](#artefact) is used as a **correlation-id** to identify where the
[payload](#payload) belongs to, e.g. to an OCI image, some source code or a
Kubernetes cluster. Also, it may be used to group multiple [Payloads](#payload)
together. The [payload](#payload) in turn holds the actual content the
extension has created, this might be for example a finding, some informational
data or some metadata (see Fig. 2).

<img src="/odg/general-overview.svg" alt="Fig. 2: General Overview">

## Artefact

The *Artefact* identifies where the [Payload](#payload) belongs to. These
*Artefacts* can be generally divided into two different groups:

* **Designtime Artefacts** *(e.g. OCI images, Helm charts, source code)*

   *Designtime artefacts* includes those artefacts which are statically
   available right after the build. Commonly, these artefacts are already
   modelled via OCM as `resources` or `sources` and can be directly translated
   into the `artefact` model of the `ArtefactMetadata`. The supported
   `artefact_kinds` are therefore `resource` and `source`.

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

* **Runtime Artefacts** *(e.g. Kubernetes clusters, hyperscaler resources)*

   *Runtime artefacts* can not be statically modelled via OCM as they are
   ephemeral in nature and not related to the build process. Hence, those
   kinds of artefacts have to be modelled more individually. An important
   aspect to consider when defining the model is that it is necessary to be
   able to unambiguously identify an artefact and that related artefacts can be
   grouped together (i.e. there must be some shared properties, e.g. the
   `artefact_type`). Some already existing examples:

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

When defining how to set the `artefact` properties, it is important to consider
that this **correlation-id** is used to find related data or to create logical
groups which may be used, for example, to group items into the same issue as
part of the GitHub issue reporting. The attributes which are used for this kind
of grouping can be configured freely, but it must be ensured that the content
of the included properties is "stable". That means, it might not be beneficial
to include a *version* property or a temporary *instance-id* as a grouping
relevant properties as this would not allow to correlate the same
[payload](#payload) between multiple versions or instances, ultimately causing for
example initial discovery dates to be re-written or new GitHub issues being
created instead of existing ones being updated. In the examples above, grouping
constellations which proved to be favorable are highlighted.

## Metadata

In general, the `meta` field holds information on where the [Payload](#payload)
comes from (`datasource`) and what type of [Payload](#payload) it is (`type`). In
most cases, the `datasource` is equivalent to the name of the extension. Both,
the `datasource` and the `type` share a global namespace. When it comes to the
`type`, it can be differentiated between three kinds of datatypes:

1. *Meta Types*

   Those datatypes are not directly related to any type of finding or a single
   extension, but rather used internally by the Open Delivery Gear. Most
   presumably the new extension does not have to define any of those datatypes.
   The most prominent one is the `meta/artefact_scan_info` which must be
   emitted by an extension for every processed [artefact](#artefact) to indicate that
   is has been successfully processed. Also, it contains information on the
   last execution in general (e.g. a timestamp or a reference) (see
   [artefact-scan-info](#artefact-scan-info) for an example). The relationship of a *meta type*
   and an [artefact](#artefact) is usually 1:1.

   *Examples:* `meta/artefact_scan_info`, `meta/responsibles`

2. *Finding Types*

   Finding types describe deviations from a desired state defined by a ruleset,
   for example the presence of a known vulnerability. Also, those finding types
   can be assigned to a certain "severity". As findings usually have to be
   resolved within a certain timeframe, those `ArtefactMetadata` entries also
   have to provide a initial discovery-date together with their
   `allowed_processing_time`. To have more control over the assignees in case
   of a reporting via GitHub issues, the `responsibles` detected by the
   extension can be also added to the `meta` field to overwrite the default
   fallback (see
   [Lifecycling GitHub Issues](/docs/concepts/odg/03-issue-replicator/)). The
   relationship of findings and an [artefact](#artefact) is typically n:1.

3. *Informational Types*

   If an extension collects data for a certain [artefact](#artefact) which is not
   considered to be a finding, it should be modelled as an informational
   datatype. The information might be used to enrich the reported findings.
   For example, in the context of vulnerabilities, an additional informational
   type holds information on the detected file paths to add the package
   location to the reporting afterwards. In this case, the information is not
   part of the [payload](#payload) of the finding type already as the relationship
   of file paths to vulnerability findings is n:n.

To create a mapping between the `Datasource` and the `Datatypes` it emits (and
vice-versa), the respective util functions `datasource()` and `datatypes()`
must be updated as well.

## Payload

The schema of the *Payload*, model-wise referred to as `data`, can be
individually defined by the extension to store the actual content. Therefore,
it is necessary to add a new dataclass with the desired structure for each
`Datatype`. However, type-definitions must be consistent for each model-element
of the same `Datatype`. Afterwards, this new dataclass must be added to the
list of allowed types for the `data` property of the `ArtefactMetadata` model
class.

### Key

To be able to unambiguously identify already existing database entries, it is
required for each `ArtefactMetadata` instance to define a unique `key`
property. This `key` always consists of the `artefact`, `Datasource`,
`Datatype` as well as the `key` defined by the `data` class (if there is any).
This means, in case it is expected that there may be multiple entries per tuple
of `artefact`, `Datasource` and `Datatype`, the new class must define a unique
`key` property as well.

> **Note:** See [gardener/cc-utils#1166](https://github.com/gardener/cc-utils/pull/1166/files)
> as an example for this chapter. Please note that the `dso.model` module in
> the pull request has been replaced by the `odg.model` module in the
> odg-core.

## Discovery Date

Findings (deviations from rulesets) typically have to be processed within an
allowed timeframe. Hence, the date of first discovery is stored to allow for
the calculation for latest due-dates. Thereby, the initial `discovery_date`
must be retained during subsequent updates. Therefore, the `discovery_date` is
part of the `ArtefactMetadata` model. By default, the initial `discovery_date`
of a finding is re-used in case the OCM identity (except its version and extra
identity) and the `key` property of the finding match. In case it is desired to
deviate from this default behaviour (e.g. in case the `key` contains a package
version which should not be considered for the re-use), a custom check must be
implemented as part of the upload metadata route.

### Considerations

In the most trivial example, this is the case when the `data` key is equal.
However, there might be cases where this is not enough, for example for
vulnerability findings, the `discovery_date` must be re-used in case the CVE
and the package is the same, even if the package-version (which is part of the
`data` key) changes. Therefore, the behaviour must be defined in the
`PUT /artefacts/metadata` route
(see [open-component-model/odg-core@6697e50](https://github.com/open-component-model/odg-core/commit/6697e5045d080d72c70b2ccaa214ffcaa8d0e244)
as an example how to define this behaviour). In case it is not defined, the
`discovery_date` will be always consumed as it is defined in the new
`ArtefactMetadata` entry.

## Examples

### Artefact Scan Info

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
