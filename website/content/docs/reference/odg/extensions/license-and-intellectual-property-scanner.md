---
title: "License and Intellectual Property Scanner"
description: "Configuration reference for the License and Intellectual Property Scanner (BlackDuck) extension."
weight: 9
toc: true
---

The License and Intellectual Property Scanner extension uploads SBoMs to BlackDuck instances and retrieves findings (including vulnerability and intellectual property findings) for tracked components.

## Configuration Example

```yaml
blackduck:
  interval: 86400                    # 24 hours
  label_rules:
    - name: gpl-license
      selector:
        license_name: 'GPL.*'        # regex pattern
        host: null
        policy_violation_id: null
    - name: critical-policy
      selector:
        policy_violation_id: 'VULN_.*'
        host: 'blackduck.prod.example.com'
        license_name: null
  mappings:
    - prefix: 'acme.org/product-a'
      targets:
        - group_id: "1234"
          host: "blackduck.prod.example.com"
          distribution_mode_overwrite: INTERNAL
        - group_id: "5678"
          host: "blackduck.compliance.example.com"
          distribution_mode_overwrite: null
      deduplicate_across_component_versions: true
      cleanup_deprecated_project_versions: false
      aws_secret_name: aws-account-prod
    - prefix: ''                     # catch-all
      targets:
        - group_id: "9999"
          host: "blackduck.example.com"
      deduplicate_across_component_versions: true
      cleanup_deprecated_project_versions: true
      aws_secret_name: aws-account-default
```

## Top-Level Options

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `interval` | int (seconds) | `86400` | Maximum time before a component is re-processed. |
| `label_rules` | list | `[]` | Label rules for categorizing findings. See label rule fields below. |
| `mappings` | list | `[]` | Per-prefix component mappings. See mapping fields below. |

## Mapping Fields

Each entry in the `mappings` list supports the following fields:

| Option | Type | Required | Description |
| --- | --- | --- | --- |
| `prefix` | string | yes | Component name prefix. Use `''` (empty string) to match all components. |
| `targets` | list | yes | List of BlackDuck instances to upload results to. See target fields below. |
| `deduplicate_across_component_versions` | bool | `true` | If true, deduplicate scans of the same artefact across component versions. |
| `cleanup_deprecated_project_versions` | bool | `false` | If true, delete BlackDuck project versions for artefacts no longer tracked. |
| `aws_secret_name` | string | no | Name of the AWS secret to use for S3 artefacts. |

## Label Rule Fields

Each entry in the `label_rules` list supports the following fields:

| Option | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | yes | Descriptive name for this labeling rule. |
| `selector` | object | yes | Selector criteria for matching findings. See selector fields below. |

## Label Rule Selector Fields

Each `selector` object supports the following fields (all optional, supports regex patterns):

| Option | Type | Required | Description |
| --- | --- | --- | --- |
| `host` | string | no | Regex pattern to match against BlackDuck host. `null` matches any host. |
| `policy_violation_id` | string | no | Regex pattern to match against policy violation IDs. `null` matches any ID. |
| `license_name` | string | no | Regex pattern to match against license names. `null` matches any license. |

## Target Fields

Each entry in the `targets` list supports:

| Option | Type | Required | Description |
| --- | --- | --- | --- |
| `group_id` | string | yes | The BlackDuck group ID. |
| `host` | string | yes | The hostname of the BlackDuck instance. |
| `distribution_mode_overwrite` | string | no | Override distribution mode: `EXTERNAL`, `INTERNAL`, `OPENSOURCE`, `SAAS`. |

## Configuration Details

### Selector Matching

All selector fields support regex patterns and use `null` as a wildcard (matches anything).
A finding matches a rule when ALL non-null selector fields match their patterns.

**Examples:**

- `license_name: 'GPL.*'` matches GPL-2.0, GPL-3.0, LGPL
- `host: 'blackduck\\.prod\\..*'` matches any production BlackDuck instance
- All fields `null` creates a catch-all rule

### `mappings`

Allows routing different components to different BlackDuck instances and applying
different processing policies.

#### Prefix Matching

The `prefix` field uses simple string prefix matching:

- `prefix: 'acme.org'` matches `acme.org/product` and `acme.org/another-product`
- `prefix: ''` (empty string) matches all components (use as a catch-all)

### `targets`

A single component can be uploaded to multiple BlackDuck instances. This is useful for:

- Uploading to both production and compliance BlackDuck instances
- Cross-regional compliance reporting
- Multi-tenant scenarios

#### Distribution Mode

The `distribution_mode_overwrite` field controls the BlackDuck project version's distribution mode:

- **`EXTERNAL`**: Software distributed to external parties
- **`INTERNAL`**: Software for internal use only
- **`OPENSOURCE`**: Open source software
- **`SAAS`**: Software-as-a-Service offerings
- **`null`** (default): Don't override the distribution mode

This metadata helps BlackDuck apply appropriate policy rules and compliance requirements.

### `deduplicate_across_component_versions`

When `true`, if the same artefact version appears in multiple component versions,
BlackDuck scans are deduplicated. This reduces redundant processing and API calls.

**Example:**

- Component v1.0.0 includes artefact `library:1.2.3`
- Component v1.1.0 also includes artefact `library:1.2.3`
- With deduplication enabled: only one BlackDuck scan for `library:1.2.3`

### `cleanup_deprecated_project_versions`

When `true`, the extension will delete BlackDuck project versions for artefacts that
are no longer referenced by any tracked component versions. This keeps BlackDuck clean
and reduces clutter.

**Use with caution:** Only enable this if you're confident that the artefact enumerator
is tracking all component versions you care about.

### `aws_secret_name`

When scanning artefacts stored in AWS S3, specify which AWS secret to use for authentication.
