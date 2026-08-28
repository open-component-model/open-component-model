---
title: "Overwriting OCM component responsibles"
description: "How the responsibles extension determines component ownership based on configurable rules."
weight: 5
toc: true
---

The responsibles extension is able to determine responsibles based on
configured `rules`, overriding or extending what is declared statically via
the `odg.ocm.software/responsibles` OCM label.
The determined responsibles are uploaded as
`ArtefactMetadata` of type `meta/responsibles`. A rule in this context is made
up by a list of `filters` and a list of assigned `strategies`. A rule is
considered to be a match if and only if all of the filters of a rule match the given
artefact + datatype. The first matching rule "wins". In case no rule matches,
no responsible objects are uploaded.

The hereby determined responsibles are then used by the
[issue-replicator extension]({{< relref "docs/concepts/odg/lifecycling-github-issues/" >}}) as option
for the GitHub issue assignees. Please refer to the documentation of the
issue-replicator for more details on the **precedence behaviour** of
responsibles.

Next to the filters and strategies, a rule can define an optional
`assignee_mode` as well. This mode defines the behavioural contract in case a
GitHub issue already has assignees which are different from those the current
execution yields.

> **Note:** In order to enable the issue-replicator extension to use these responsibles
> objects as source to determine GitHub issue assignees, the responsibles must
> contain the same `github_hostname` as the target GitHub issue repository. In
> case none of the found responsibles has the correct hostname, the GitHub
> issue won't have any updated assignees.

## Examples

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

### Artefact Metadata

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
