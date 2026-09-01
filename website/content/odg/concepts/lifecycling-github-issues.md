---
title: "Lifecycling GitHub Issues"
description: "How the issue-replicator extension manages GitHub issue lifecycle for ODG findings."
weight: 4
toc: true
---

## What is the issue-replicator?

The issue-replicator is an ODG extension responsible for the complete GitHub issue lifecycle — create, update, and close — for issues related to configured artefacts and finding types. It translates ODG findings into GitHub issues, keeps them up to date as findings change, and closes them when findings are resolved or artefacts go out of scope. If enabled, it also assigns responsible team members to each issue.

## Why does it exist?

Security and compliance findings need to reach the teams that own the affected components. Manually creating and maintaining GitHub issues for hundreds of findings across many components is not scalable. The issue-replicator automates this: it provides a stable, auditable link between ODG findings and GitHub issues, grouping related findings to reduce noise and closing issues automatically when the underlying problem is resolved.

## How it works

To identify already-existing GitHub issues, the issue-replicator creates a **stable id** and adds it as a label to managed issues. This label is used to query existing issues. The issue id is composed of the **grouping-relevant properties** of the `artefact` (see [Artefact Groups](#artefact-groups)) and the **due date**. A version prefix is added to differentiate issue ids if their calculation changes in the future.

A GitHub issue always comprises **all findings** of a single **finding type** for an [artefact group](#artefact-groups) that are due in the same **sprint**. This behaviour can be changed to create one GitHub issue per finding (no grouping) by setting the `enable_per_finding` flag in the respective findings configuration.

### Artefact Groups

Artefact groups are defined by the properties configured in `attrs_to_group_by` per type in the findings configuration. All GitHub issues related to artefacts in the group are updated at once when a respective backlog item is processed. However, the `artefact` defined in the backlog item **must** contain (at least) all grouping-relevant properties. The non-grouping-relevant properties of the backlog item are only used when the findings configuration has a `filter` configured.

To find all artefacts associated with the group, all [compliance snapshots](#compliance-snapshots) with matching artefact group properties are retrieved and their `artefact` information is used.

### Compliance Snapshots

Compliance snapshots store the state of components that are intended to be processed periodically (see [artefact-enumerator extension]({{< relref "docs/concepts/odg/reacting-upon-ocm-events/" >}}) for more details). To prevent GitHub issues being created for components that are not of interest (e.g. if a scan and issue update were triggered manually), the **issue-replicator requires compliance snapshots** to be present for the artefact group. If there is not at least one "active" compliance snapshot for the artefact group, the group is considered not of interest (anymore), and all associated GitHub issues (if any) will be closed.

{{< callout type="note" >}}
Compliance snapshots are not intended to be managed manually. They are managed exclusively via the [artefact-enumerator extension]({{< relref "docs/concepts/odg/reacting-upon-ocm-events/" >}}).
{{< /callout >}}

<img src="/odg/issues-overview.svg" alt="Issues Overview">

### GitHub Issue Assignees

If the `enable_assignees` flag is set in the respective findings configuration, the issue-replicator will try to determine responsibles for the artefact group and, if any are found, assign them to the GitHub issues. The following **lookup precedence** applies (if one lookup yields `None`, the next is tried; if a lookup yields an empty list `[]`, this is interpreted as "no responsibles" and no further lookup is performed):

1. **Overwrites**

   When uploading `ArtefactMetadata` of type `meta/artefact_scan_info`, extensions may add responsible information via the `.meta.responsibles` attribute and a corresponding assignee mode via `.meta.assignee_mode`.

2. **Extension**

   The [responsibles extension]({{< relref "docs/concepts/odg/overwriting-ocm-component-responsibles/" >}}) tries to resolve responsibles by examining configured `rules` for the components of interest. If a rule matches, the responsibles retrieved via the configured `strategies` are uploaded as `ArtefactMetadata` of type `meta/responsibles`, with the `.meta` attribute used as in (1).

3. **Delivery-Service**

   The responsibles retrieved from the ODG API `/ocm/component/responsibles` are used as a last fallback. These are not persisted as `ArtefactMetadata` but calculated ad-hoc (or consumed from persistent cache).

The **behavioural contract** when a GitHub issue already has assignees that differ from those yielded by the current execution can be defined via the `assignee_mode`. In (1) and (2), this mode can be set via the `.meta` property. In (3), or if it is not set (or explicitly set to `None`), the `default_assignee_mode` configured in the respective findings configuration is used.

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

## Key properties

| Property | Description |
| --- | --- |
| Grouping | By finding type, artefact group, and sprint/due date |
| Stable ID | Label derived from grouping properties + due date; version-prefixed |
| Compliance snapshot requirement | At least one active snapshot must exist for a group |
| Assignee precedence | Extension overwrite → responsibles extension → ODG API fallback |
| Per-finding mode | Configurable via `enable_per_finding` flag |
| Issue closure | Automatic when artefact group has no active compliance snapshots |

## Relationship to other concepts

- [ODG System Architecture]({{< relref "docs/concepts/odg/odg-system-architecture/" >}}) — describes where the issue-replicator sits in the overall component topology
- [Correlating metadata to OCM]({{< relref "docs/concepts/odg/correlating-metadata-to-ocm/" >}}) — defines the `ArtefactMetadata` finding types that the issue-replicator consumes
- [Reacting upon OCM events]({{< relref "docs/concepts/odg/reacting-upon-ocm-events/" >}}) — manages the compliance snapshots that the issue-replicator depends on
- [Overwriting OCM component responsibles]({{< relref "docs/concepts/odg/overwriting-ocm-component-responsibles/" >}}) — the second priority in the assignee lookup chain

## When to use it

Refer to this document when:

- You are **configuring GitHub issue creation** for a finding type and need to understand grouping
- You are **debugging why an issue was not created or closed** (compliance snapshot state, artefact group properties)
- You are **setting up assignee resolution** for issues and need to understand the precedence rules
- You want to understand how **sprint-based issue grouping** works

## Next steps

- [Deploying the Open Delivery Gear locally]({{< relref "docs/how-to/odg/deploying-the-open-delivery-gear-locally/" >}})
- [Prepare your component for ODG]({{< relref "docs/how-to/odg/prepare-your-component-for-odg/" >}})
- [Contribute a new Extension]({{< relref "docs/tutorials/odg/contribute-a-new-extension/" >}})

## Related documentation

- [ODG System Architecture]({{< relref "docs/concepts/odg/odg-system-architecture/" >}})
- [Correlating metadata to OCM]({{< relref "docs/concepts/odg/correlating-metadata-to-ocm/" >}})
- [Reacting upon OCM events]({{< relref "docs/concepts/odg/reacting-upon-ocm-events/" >}})
- [Overwriting OCM component responsibles]({{< relref "docs/concepts/odg/overwriting-ocm-component-responsibles/" >}})
- [Generating Software Bill of Materials]({{< relref "docs/concepts/odg/generating-software-bill-of-materials/" >}})
- [SLA Violation Profiler]({{< relref "docs/concepts/odg/sla-violation-profiler/" >}})
