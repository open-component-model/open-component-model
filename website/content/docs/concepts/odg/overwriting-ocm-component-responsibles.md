---
title: "Overwriting OCM component responsibles"
description: "How the responsibles extension determines component ownership based on configurable rules."
weight: 5
toc: true
---

## What is the responsibles extension?

The responsibles extension determines who is responsible for an OCM component or artefact based on a set of configurable rules, overriding or extending what is declared statically via the `odg.ocm.software/responsibles` OCM label. It uploads the determined responsible persons or teams as `ArtefactMetadata` of type `meta/responsibles`, making them available for use by the issue-replicator when assigning GitHub issues.

## Why does it exist?

Static ownership labels in OCM component descriptors cannot always capture the full picture: a single vulnerability finding type may need a different owner than the component's general maintainers, or ownership may need to differ per artefact type. The responsibles extension provides a rule-based mechanism to express these nuances without modifying component descriptors, and uploads the result as structured metadata that downstream extensions can consume predictably.

## How it works

A rule is made up of a list of `filters` and a list of assigned `strategies`. A rule matches if and only if **all** of its filters match the given artefact and datatype. The **first matching rule wins**. If no rule matches, no responsible objects are uploaded.

Each rule can also define an optional `assignee_mode`, which defines the behavioural contract when a GitHub issue already has assignees that differ from those yielded by the current execution.

The determined responsibles are used by the [issue-replicator extension]({{< relref "docs/concepts/odg/lifecycling-github-issues/" >}}) as one option for GitHub issue assignees. Refer to the issue-replicator documentation for details on the **assignee lookup precedence**.

{{< callout type="note" >}}
For the issue-replicator to use these responsibles objects as a source for GitHub issue assignees, the responsibles must contain the same `github_hostname` as the target GitHub issue repository. If none of the found responsibles has the correct hostname, the GitHub issue will not have any updated assignees.
{{< /callout >}}

### Configuration

```yaml
responsibles:
  rules:
    - name: vulnerability-responsibles
      filters:
        - type: datatype-filter
          include_types:
            - finding/vulnerability
      strategies:
        - type: static-responsibles
          responsibles:
            - type: githubTeam
              github_hostname: github.com
              teamname: my-teamname
            - type: githubUser
              github_hostname: github.com
              username: my-username
      assignee_mode: overwrite
    - name: special-image-responsibles
      filters:
        - type: component-filter
          include_component_names:
            - example.org/my-component
        - type: artefact-filter
          include_artefact_types:
            - ociImage
      strategies:
        - type: static-responsibles
          responsibles:
            - type: githubTeam
              github_hostname: github.com
              teamname: my-other-teamname
      assignee_mode: extend
    - name: remainder
      filters:
        - type: match-all
      strategies:
        - type: component-responsibles
      assignee_mode: skip
```

### Produced ArtefactMetadata

The extension uploads `ArtefactMetadata` of type `meta/responsibles`. The `data.referenced_type` field records which finding type these responsibles apply to:

```yaml
artefact:
  component_name: example.org/my-component
  component_version: 0.1.0
  artefact_kind: resource
  artefact:
    artefact_name: my-resource
    artefact_version: 0.1.0
    artefact_type: ociImage
    artefact_extra_id:
      version: 0.1.0
meta:
  type: meta/responsibles
  datasource: responsibles
  responsibles:
    - identifiers:
        - type: githubUser
          source: responsibles
          github_hostname: github.com
          username: my-username
    - identifiers:
        - type: githubUser
          source: responsibles
          github_hostname: github.com
          username: my-second-username
  assignee_mode: extend
data:
  referenced_type: finding/vulnerability
```

## Key properties

| Property | Description |
| --- | --- |
| Rule matching | First matching rule wins; all filters in a rule must match |
| No match | No `meta/responsibles` entry is uploaded |
| Assignee mode | Per-rule; controls behaviour when existing assignees differ |
| Output type | `ArtefactMetadata` of type `meta/responsibles` |
| Hostname constraint | Responsibles must match the target GitHub repository's hostname |
| Override scope | Overrides the static `odg.ocm.software/responsibles` OCM label |

## Relationship to other concepts

- [ODG System Architecture]({{< relref "docs/concepts/odg/odg-system-architecture/" >}}) — describes where the responsibles extension fits in the overall component topology
- [Correlating metadata to OCM]({{< relref "docs/concepts/odg/correlating-metadata-to-ocm/" >}}) — defines `ArtefactMetadata` and the `meta/responsibles` type
- [Lifecycling GitHub Issues]({{< relref "docs/concepts/odg/lifecycling-github-issues/" >}}) — consumes `meta/responsibles` entries as the second priority in the assignee lookup chain
- [Reacting upon OCM events]({{< relref "docs/concepts/odg/reacting-upon-ocm-events/" >}}) — triggers the responsibles extension via backlog items

## When to use it

Refer to this document when:

- You want to **assign different owners** to different finding types or artefact types
- You are **debugging why GitHub issues have incorrect or missing assignees**
- You need to understand how the **rule matching and first-match-wins** logic works
- You want to override the static `odg.ocm.software/responsibles` OCM label for certain artefacts

## Next steps

- [Deploying the Open Delivery Gear locally]({{< relref "docs/how-to/odg/deploying-the-open-delivery-gear-locally/" >}})
- [Prepare your component for ODG]({{< relref "docs/how-to/odg/prepare-your-component-for-odg/" >}})
- [Contribute a new Extension]({{< relref "docs/tutorials/odg/contribute-a-new-extension/" >}})

## Related documentation

- [ODG System Architecture]({{< relref "docs/concepts/odg/odg-system-architecture/" >}})
- [Correlating metadata to OCM]({{< relref "docs/concepts/odg/correlating-metadata-to-ocm/" >}})
- [Reacting upon OCM events]({{< relref "docs/concepts/odg/reacting-upon-ocm-events/" >}})
- [Lifecycling GitHub Issues]({{< relref "docs/concepts/odg/lifecycling-github-issues/" >}})
- [Generating Software Bill of Materials]({{< relref "docs/concepts/odg/generating-software-bill-of-materials/" >}})
- [SLA Violation Profiler]({{< relref "docs/concepts/odg/sla-violation-profiler/" >}})
