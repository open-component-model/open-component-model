---
title: "SLA Violation Profiler"
description: "Configuration reference for the SLA Violation Profiler extension."
weight: 17
toc: true
---

The SLA Violation Profiler evaluates whether findings for configured OCM root components were resolved or rescored within their SLA-mandated processing times, and persists the results as `sla_violation` records in the ODG database for audit reporting.

## Configuration Example

```yaml
sla_violation_profiler:
  components:
    - component_name: acme.org/my-product
      ocm_repo_url: europe-docker.pkg.dev/acme/releases
      version: greatest
      max_versions_limit: 10
      time_range:
        days_from: -365
        days_to: 0
```

## Top-Level Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `components` | list | — | List of OCM root components to evaluate. See component fields below. |

## Component Fields

Each entry in the `components` list supports the following fields:

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `component_name` | string | yes | OCM component name to evaluate (e.g., `acme.org/my-product`). |
| `version` | string | no | Specific version to evaluate, or `greatest` to use the most recent version. If omitted, `time_range` must be specified. |
| `ocm_repo_url` | string | no | Override default OCM repository lookup. |
| `max_versions_limit` | int | no | Maximum number of versions to evaluate per run. Only relevant when `version` is `greatest`. Defaults to `1`. |
| `time_range` | object | no | Restricts version discovery to a date range. See time range fields below. |

## Time Range Fields

The `time_range` object restricts which component versions are considered during version discovery. Dates are computed relative to the current day at runtime.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `days_from` | int | `-365` | Start of the time range in days relative to today (negative = past). |
| `days_to` | int | `0` | End of the time range in days relative to today (`0` = today). |

## Configuration Details

### `components`

Each entry defines one OCM root component to be profiled. The profiler traverses
the full component graph of each version to collect findings across all transitively
referenced components.

#### `version`

Controls which component versions are evaluated:

- **Fixed version** (e.g., `1.2.3`): Evaluates exactly that version. Any configured `time_range` is ignored.
- **`greatest`**: Resolves to the most recent version(s) available. Combined with
  `max_versions_limit`, this caps how many versions are processed per run.
  Any configured `time_range` is ignored.
- **Omitted**: Version discovery relies entirely on `time_range` to determine which
  versions to evaluate.

#### `max_versions_limit`

Caps the number of versions resolved when `version` is `greatest`. Increase this
value to backfill SLA records for a larger history in a single run.

```yaml
components:
  - component_name: acme.org/my-product
    version: greatest
    max_versions_limit: 52 # evaluate up to 52 versions per run
```

#### `time_range`

Restricts version discovery to a rolling date window. Both values are offsets
in days relative to the current date at the time of the run:

```yaml
time_range:
  days_from: -90 # versions created from 90 days ago
  days_to: 0 # up to today
```
