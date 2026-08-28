---
title: "Artefact Enumerator"
description: "Configuration reference for the Artefact Enumerator extension."
weight: 3
toc: true
---

The Artefact Enumerator extension is a cronjob which regularly creates backlog work-items for tracked OCM components. It monitors specified components and versions, creating work for other extensions to process.

## Configuration Example

```yaml
artefact_enumerator:
  compliance_snapshot_grace_period: 86400  # 24 hours
  schedule: '*/5 * * * *'                   # every 5 minutes
  successful_jobs_history_limit: 1
  failed_jobs_history_limit: 1
  components:
    - component_name: ocm.software/open-delivery-gear
      version: greatest               # or specific version like '0.1.0'
      max_versions_limit: 1
    - component_name: acme.org/my-component
      version:
        source:
          type: github
          repo: github.com/acme/my-repo
          relpath: [VERSION]
          ref: main
          postprocess: false
      ocm_repo_url: https://my-ocm-repo.example.com
      max_versions_limit: 3
```

## Top-Level Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `compliance_snapshot_grace_period` | int (seconds) | `86400` | Grace period for compliance snapshots before triggering re-evaluation. |
| `schedule` | string | `*/5 * * * *` | Cron schedule for running the cronjob (every 5 minutes by default). |
| `successful_jobs_history_limit` | int | `1` | Number of successful job executions to retain in history. |
| `failed_jobs_history_limit` | int | `1` | Number of failed job executions to retain in history. |
| `components` | list | `[]` | List of OCM components to track. See component fields below. |

## Component Fields

Each entry in the `components` list supports the following fields:

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `component_name` | string | yes | Name of the OCM component (e.g., `ocm.software/open-delivery-gear`). |
| `version` | string or object | yes | Component version. Use `greatest` or `null` for latest, a specific version string, or a source object to read from GitHub. |
| `ocm_repo_url` | string | no | Override the default OCM repository lookup via `ocm_repo_mappings`. |
| `max_versions_limit` | int | `1` | Number of versions that should be tracked for this component. |

### Version Source Fields

When `version` is an object with a `source` key, it supports:

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `source.type` | string | yes | Type of version location. Currently supports `github`. |
| `source.repo` | string | yes | GitHub repository name (e.g., `github.com/org-name/repo-name`). |
| `source.relpath` | list | yes | Relative path from repository root to the version file. Can include submodule specifications. |
| `source.ref` | string | no | Repository reference (branch, tag, or commit hash). Defaults to the default branch. |
| `source.postprocess` | bool | `false` | If true, appends the current commit SHA to the version (`{version}-{sha}`). |

## Configuration Details

### `compliance_snapshot_grace_period`

The grace period (in seconds) before a compliance snapshot triggers re-evaluation of components.
Default is 86400 seconds (24 hours). This prevents excessive re-processing when compliance
status hasn't meaningfully changed.

### `schedule`

The cron schedule expression that determines when the Artefact Enumerator cronjob runs.
Default is `*/5 * * * *` (every 5 minutes).

**Common schedule patterns:**

```yaml
# Every 5 minutes (default)
schedule: '*/5 * * * *'

# Every 10 minutes
schedule: '*/10 * * * *'

# Every hour
schedule: '0 * * * *'

# Every 30 minutes
schedule: '*/30 * * * *'
```

### `successful_jobs_history_limit`

Number of successful cronjob executions to retain in Kubernetes history. Default is `1`.
This controls how many completed job pods are kept for auditing and debugging purposes.

### `failed_jobs_history_limit`

Number of failed cronjob executions to retain in Kubernetes history. Default is `1`.
This controls how many failed job pods are kept for troubleshooting purposes.

### `components`

List of OCM components that ODG should track and process. Each component generates
backlog work-items that trigger scans and assessments by other extensions.

#### Version Specification

The `version` field supports three formats:

1. **Latest version** - use `greatest` or `null`:
   ```yaml
   version: greatest
   ```

2. **Specific version** - use a version string:
   ```yaml
   version: 0.1.0
   ```

3. **Dynamic version from GitHub** - read version from a file in a GitHub repository:
   ```yaml
   version:
     source:
       type: github
       repo: github.com/acme/my-repo
       relpath: [VERSION]
       ref: main
       postprocess: false
   ```

#### Version from GitHub with Submodules

For repositories with submodules, specify the submodule path in the `relpath` list:

```yaml
version:
  source:
    type: github
    repo: github.com/org-name/repo-name
    relpath:
      - type: submodule
        name: path/to/submodule
      - path/to/version-file
    ref: main
```

### `max_versions_limit`

Specifies how many historical versions of a component should be tracked and processed.
Setting this to a higher value allows tracking findings across multiple component versions,
which is useful for understanding trends and ensuring older versions meet compliance requirements.

**Examples:**
- `max_versions_limit: 1` - track only the latest version
- `max_versions_limit: 3` - track the three most recent versions
