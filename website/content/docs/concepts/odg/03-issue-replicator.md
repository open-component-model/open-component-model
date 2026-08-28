---
title: "Lifecycling GitHub Issues"
description: "How the issue-replicator extension manages GitHub issue lifecycle for ODG findings."
weight: 4
toc: true
---

The issue-replicator extension is responsible for the GitHub issue lifecycle
(create/update/close) for issues of the configured artefacts and finding
types. If enabled, it will also take care of assigning responsibles to the
GitHub issue.

A GitHub issue always comprises **all findings** of a single **finding type**
for an [artefact group](#artefact-groups) which are due in the same
**sprint**. This behaviour can be changed to create one GitHub issue per
finding (no grouping) instead by setting the `enable_per_finding` flag in the
respective findings configuration.

## How it works

To identify already existing GitHub issues, the issue-replicator creates a
**stable id** and adds it as label to the managed GitHub issues. This label is
later used to query already existing issues. The issue id is made up by the
**grouping relevant properties** of the `artefact` (see
[artefact groups](#artefact-groups)) as well as the **due date**. Also, a
version prefix is added to be able to differentiate issue ids in case their
calculation changes in the future.

### Artefact Groups

Artefact groups are defined by those properties configured in
`attrs_to_group_by` per type in the findings configuration. All GitHub issues
related to the artefacts in the group are updated at once upon processing of a
respective backlog item. However, the `artefact` defined in this backlog item
**must** contain (at least) all grouping relevant properties! The not grouping
relevant properties of the backlog item are only used in case the findings
configuration has a respective `filter` configured. To find all artefacts
associated to the group, all [compliance snapshots](#issue-replicator-compliance-snapshots)
with matching artefact group properties are retrieved and their `artefact`
information is used.

### Compliance Snapshots

Compliance snapshots are used to store the state of components which are
intended to be processed periodically, e.g. scanned, reported, etc. (see
[artefact-enumerator extension](/docs/concepts/odg/02-artefact-enumerator/) for more
details). To prevent GitHub issues being created for components which are not
of interest (e.g. if a scan and issue update were triggered manually), the
**issue-replicator requires compliance snapshots** to be present for the
artefact group. If there is not at least one "active" compliance snapshot for
the artefact group, it is considered to be not of interest (anymore), and all
associated GitHub issues (if there are any) will be closed. However, please
note that compliance snapshots are not intended to be managed manually but only
via the [artefact-enumerator extension](/docs/concepts/odg/02-artefact-enumerator/).

<img src="/odg/issues-overview.svg" alt="Issues Overview">

### GitHub Issue Assignees

If the `enable_assignees` flag is set in the respective findings configuration,
the issue-replicator will try to determine the responsibles for the artefact
group and if there are any, assign those to the GitHub issues. Since there are
different ways, where and how responsible information is stored, the following
**lookup precedence** applies (in case one lookup yields `None`, the next one
is tried, but if one lookup yields an empty list `[]`, this is interpreted as
"no responsibles" and no further lookup is performed):

1. *Overwrites*

   When uploading `ArtefactMetadata` of type `meta/artefact_scan_info`,
   extensions may add information on responsibles via the `.meta.responsibles`
   attribute and a corresponding assignee mode via `.meta.assignee_mode`.

2. *Extension*

   The [responsibles extension](/docs/concepts/odg/04-responsibles/) tries to
   resolve responsibles by examining configured `rules` for the components of
   interest. If a rule matches, the responsibles retrieved via the configured
   `strategies` are uploaded as `ArtefactMetadata` of type
   `meta/responsibles`, with the `.meta` attribute used like in (1).

3. *Delivery-Service*

   The responsibles retrieved from the ODG API
   `/ocm/component/responsibles` are used as last fallback. Those are not
   persisted as `ArtefactMetadata` but calculated ad-hoc (or consumed from
   persistent cache).

The **behavioural contract** in case a GitHub issue already has assignees which
are different from those the current execution yields, can be defined via the
`assignee_mode`. In (1) and (2), this mode can be set via the `.meta` property.
In (3) or if it is not set (or explicitly set to `None`), the
`default_assignee_mode` configured in the respective findings configuration is
used.

## Examples

### Configuration

```yaml
issue_replicator:
  mappings:
    - prefix: example.org/my-component
      github_repository: github.com/my-organisation/my-repository
      github_issue_labels_to_preserve:
        - never-remove-this-label
      number_included_closed_issues: 100
      milestones:
        title:
          prefix: week-
          suffix: ''
          sprint:
            value_type: date
            date_name: end_date
            date_string_format: '%V' # week number
        due_date:
          date_name: release_decision
    - prefix: ''
      github_repository: github.com/my-organisation/my-repository
```

### Finding Type Configuration

```yaml
- type: finding/vulnerability
  issues:
    enable_issues: True
    enable_per_finding: False
    enable_assignees: True
    default_assignee_mode: skip
    template: '{summary}'
    title_template: '[{meta.type}] - {artefact.component_name}:{artefact.artefact.artefact_name}'
    labels:
      - this-label-is-assigned-to-every-issue
    attrs_to_group_by:
      - component_name
      - artefact_kind
      - artefact.artefact_name
      - artefact.artefact_type
```
