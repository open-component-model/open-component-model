---
title: "Responsibles"
description: "The Responsibles extension determines component ownership based on configurable rules. It assigns responsible teams or individuals to findings, enabling…"
weight: 160
toc: true
---
The Responsibles extension determines component ownership based on configurable rules. It assigns responsible teams or individuals to findings, enabling proper routing of security and compliance issues.

## Configuration Example

```yaml
responsibles:
  interval: 43200                    # 12 hours
  rules:
    # Rule 1: Frontend team owns all frontend components
    - name: frontend-team-ownership
      assignee_mode: overwrite
      filters:
        - type: component-filter
          include_component_names:
            - "acme.org/frontend/.*"
          exclude_component_names: []
      strategies:
        - type: static-responsibles
          responsibles:
            - type: githubTeam
              github_hostname: github.com
              teamname: acme-org/frontend-team

    # Rule 2: Security team owns all vulnerability findings
    - name: security-team-vulnerabilities
      assignee_mode: extend
      filters:
        - type: datatype-filter
          include_types:
            - "finding/vulnerability"
          exclude_types: []
      strategies:
        - type: static-responsibles
          responsibles:
            - type: githubTeam
              github_hostname: github.com
              teamname: acme-org/security-team

    # Rule 3: Platform team owns container images
    - name: platform-team-containers
      assignee_mode: overwrite
      filters:
        - type: artefact-filter
          include_artefact_types:
            - "ociImage"
          exclude_artefact_types: []
      strategies:
        - type: static-responsibles
          responsibles:
            - type: githubTeam
              github_hostname: github.com
              teamname: acme-org/platform-team

    # Rule 4: Fallback to component-defined responsibles
    - name: default-component-responsibles
      assignee_mode: null
      filters:
        - type: match-all
      strategies:
        - type: component-responsibles
```

## Top-Level Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `interval` | int (seconds) | `43200` | Maximum time before component responsibles are re-determined. |
| `rules` | list | `[]` | Responsibility assignment rules. See rule fields below. |

## Rule Fields

Each entry in the `rules` list supports:

| Option | Type | Required | Description |
| -------- | ------ | ---------- | ------------- |
| `name` | string | no | Rule name for logging and debugging purposes. |
| `assignee_mode` | string | no | How to handle existing GitHub issue assignees. Options: `null`, `extend`, `overwrite`, `skip`. |
| `filters` | list | yes | Conditions that must match for this rule to apply. |
| `strategies` | list | yes | How to determine responsibles when filters match. |

## Filter Types

Filters determine when a rule applies. Multiple filters are combined with AND logic.

### `match-all` Filter

Matches everything (useful for fallback rules):

```yaml
filters:
  - type: match-all
```

### `component-filter` Filter

Matches based on component name:

```yaml
filters:
  - type: component-filter
    include_component_names:
      - "acme.org/.*"              # Regex: all acme.org components
      - "example.com/product-a"    # Specific component
    exclude_component_names:
      - "acme.org/deprecated/.*"   # Exclude deprecated components
```

### `artefact-filter` Filter

Matches based on artefact properties:

```yaml
filters:
  - type: artefact-filter
    include_artefact_names:
      - "my-service"
    exclude_artefact_names: []
    include_artefact_types:
      - "ociImage"                 # Container images
      - "helm"                     # Helm charts
    exclude_artefact_types: []
    include_artefact_kinds:
      - "source"                   # Source code artefacts
    exclude_artefact_kinds: []
```

### `datatype-filter` Filter

Matches based on finding type:

```yaml
filters:
  - type: datatype-filter
    include_types:
      - "finding/vulnerability"
      - "finding/malware"
    exclude_types:
      - "finding/license"          # License issues handled separately
```

## Strategy Types

Strategies determine how responsibles are assigned when filters match.

### `component-responsibles` Strategy

Use responsibles defined in the component metadata via the delivery-service API:

```yaml
strategies:
  - type: component-responsibles
```

**Use case:** Components define their own ownership in OCM metadata

### `static-responsibles` Strategy

Explicitly define responsibles in the rule:

```yaml
strategies:
  - type: static-responsibles
    responsibles:
      - type: githubTeam
        github_hostname: github.com
        teamname: acme-org/backend-team
      - type: githubUser
        github_hostname: github.com
        username: security-lead
```

## Responsible Types

### GitHub Team

Assign responsibility to all members of a GitHub team:

```yaml
- type: githubTeam
  github_hostname: github.com
  teamname: org-name/team-slug      # Format: organization/team-slug
```

**Example:**

```yaml
- type: githubTeam
  github_hostname: github.com
  teamname: acme-org/frontend-engineers
```

### GitHub User

Assign responsibility to a specific GitHub user:

```yaml
- type: githubUser
  github_hostname: github.com
  username: john-doe
```

**Use case:** Individual ownership for critical components or specialized findings

## Assignee Modes

Controls how determined responsibles interact with existing GitHub issue assignees:

| Mode | Behaviour |
| ------ | ---------- |
| `null` | Use the default mode from finding configuration (`findings[].issues.default_assignee_mode`). |
| `extend` | Add determined responsibles to existing issue assignees (union). |
| `overwrite` | Replace all existing assignees with determined responsibles. |
| `skip` | Don't modify issue assignees (responsibles tracked in ODG only). |

## Configuration Details

### Finding-Type-Based Assignment

Route findings by severity or type:

```yaml
responsibles:
  interval: 43200
  rules:
    # Critical vulnerabilities go to security team
    - name: critical-vulns-to-security
      assignee_mode: extend           # Add security team to existing owners
      filters:
        - type: datatype-filter
          include_types: ["finding/vulnerability"]
      strategies:
        - type: static-responsibles
          responsibles:
            - type: githubTeam
              github_hostname: github.com
              teamname: acme-org/security-team

    # Malware findings go to security team exclusively
    - name: malware-to-security
      assignee_mode: overwrite        # Security team takes full ownership
      filters:
        - type: datatype-filter
          include_types: ["finding/malware"]
      strategies:
        - type: static-responsibles
          responsibles:
            - type: githubTeam
              github_hostname: github.com
              teamname: acme-org/security-team
```

### Artefact-Type-Based Assignment

Route based on what's being scanned:

```yaml
responsibles:
  interval: 43200
  rules:
    # Container images owned by platform team
    - name: containers-to-platform
      assignee_mode: overwrite
      filters:
        - type: artefact-filter
          include_artefact_types: ["ociImage"]
      strategies:
        - type: static-responsibles
          responsibles:
            - type: githubTeam
              github_hostname: github.com
              teamname: acme-org/platform-team

    # Helm charts owned by release team
    - name: helm-to-release
      assignee_mode: overwrite
      filters:
        - type: artefact-filter
          include_artefact_types: ["helm"]
      strategies:
        - type: static-responsibles
          responsibles:
            - type: githubTeam
              github_hostname: github.com
              teamname: acme-org/release-team
```

### Hybrid: Static + Dynamic

Combine static rules with component-defined ownership:

```yaml
responsibles:
  interval: 43200
  rules:
    # Security team always involved in vulnerabilities
    - name: security-oversight
      assignee_mode: extend           # Add to component-defined owners
      filters:
        - type: datatype-filter
          include_types: ["finding/vulnerability", "finding/malware"]
      strategies:
        - type: static-responsibles
          responsibles:
            - type: githubTeam
              github_hostname: github.com
              teamname: acme-org/security-team

    # Everything else uses component-defined ownership
    - name: component-default
      assignee_mode: null
      filters:
        - type: match-all
      strategies:
        - type: component-responsibles
```
