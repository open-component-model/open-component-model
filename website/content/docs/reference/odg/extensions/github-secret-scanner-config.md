---
title: "GitHub Secret Scanner"
description: "The GitHub Secret Scanner extension is a cronjob that regularly checks GitHub secret alerts and manages the lifecycle of respective findings."
weight: 140
toc: true
---
The GitHub Secret Scanner extension is a cronjob that regularly checks GitHub secret alerts and manages the lifecycle of respective findings.

## Configuration Example

```yaml
ghas:
  on_unsupported: warning
  schedule: '0 0 * * *'           # daily at midnight
  successful_jobs_history_limit: 1
  failed_jobs_history_limit: 1
  github_instances:
    - hostname: github.com
      orgs:
        - open-component-model
        - acme-org
    - hostname: github.acme.org
      orgs:
        - internal-team
```

## Top-Level Options

| Option | Type | Default | Description |
| -------- | ------ | --------- | ------------- |
| `on_unsupported` | string | `warning` | Behaviour when artefact kind/type/access is unsupported. Options: `fail`, `ignore`, `warning`. |
| `schedule` | string | `0 0 * * *` | Cron schedule for running the cronjob (daily at midnight by default). |
| `successful_jobs_history_limit` | int | `1` | Number of successful job executions to retain in history. |
| `failed_jobs_history_limit` | int | `1` | Number of failed job executions to retain in history. |
| `github_instances` | list | `[]` | List of GitHub instances to monitor. See github_instances fields below. |

## GitHub Instances Fields

Each entry in the `github_instances` list supports the following fields:

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `hostname` | string | yes | Hostname of the GitHub instance (e.g., `github.com`, `github.enterprise.com`). |
| `orgs` | list | yes | List of GitHub organizations to fetch secret alerts for. |

## Configuration Details

### `on_unsupported`

Defines the behaviour when an artefact kind, type, or access method is not supported:

- **`fail`**: Raise an exception and stop processing
- **`ignore`**: Silently skip the unsupported artefact
- **`warning`** (default): Skip the artefact and log a warning message

### `schedule`

The cron schedule expression that determines when the GitHub Secret Scanner cronjob runs.
Default is `0 0 * * *` (daily at midnight).

**Common schedule patterns:**

```yaml
# Every day at midnight
schedule: '0 0 * * *'

# Every 6 hours
schedule: '0 */6 * * *'

# Every Monday at 9 AM
schedule: '0 9 * * 1'

# Every hour
schedule: '0 * * * *'
```

### `successful_jobs_history_limit`

Number of successful cronjob executions to retain in Kubernetes history. Default is `1`.
This controls how many completed job pods are kept for auditing and debugging purposes.

### `failed_jobs_history_limit`

Number of failed cronjob executions to retain in Kubernetes history. Default is `1`.
This controls how many failed job pods are kept for troubleshooting purposes.

### `github_instances`

Configures which GitHub instances and organizations to monitor for secret alerts. This allows
monitoring across multiple GitHub deployments (public GitHub and GitHub Enterprise instances).

#### Multiple Instance Support

You can configure multiple GitHub instances to monitor different organizational boundaries:

```yaml
github_instances:
  - hostname: github.com
    orgs:
      - my-open-source-org
      - another-public-org
  - hostname: github.enterprise.acme.com
    orgs:
      - internal-security
      - platform-team
```

#### Hostname

The hostname of the GitHub instance. For public GitHub, use `github.com`. For GitHub Enterprise
Server installations, use your enterprise hostname (e.g., `github.acme.org`).

#### Organizations

List of GitHub organization names to monitor for secret alerts. The GHAS extension will check
all repositories within these organizations that have GitHub Advanced Security enabled.

**Example with multiple organizations:**

```yaml
ghas:
  github_instances:
    - hostname: github.com
      orgs:
        - ocm-project
        - security-team
        - compliance-team
```
