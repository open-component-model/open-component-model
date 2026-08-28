---
title: "Findings Report Generator"
description: "Configuration reference for the Findings Report Generator extension."
weight: 13
toc: true
---

The Findings Report Generator extension generates and publishes compliance and security reports to GitHub repositories. It can create overview reports and detailed per-version reports for specified finding types and components.

## Configuration Example

```yaml
findings_report:
  schedule: '0 2 * * *'              # daily at 2 AM
  successful_jobs_history_limit: 1
  failed_jobs_history_limit: 1
  mappings:
    - type: finding/malware
      component:
        component_name: acme.org/my-product
        ocm_repo_url: null
        time_range:
          days_from: -365          # Last 365 days
          days_to: 0
      github_repository: github.com/acme/security-reports
      branch: gh-pages
      filename: malware-report.md
      dirname: malware-reports
      auto_merge: false
      trigger_absent_scans: false
      report_to_saf: false
    - type: finding/vulnerability
      component:
        component_name: acme.org/my-product
        time_range:
          days_from: -90           # Last 90 days
          days_to: 0
      github_repository: github.com/acme/security-reports
      branch: main
      filename: vulnerability-overview.md
      dirname: vuln-reports
      auto_merge: true
      trigger_absent_scans: true
      report_to_saf: false
```

## Top-Level Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `schedule` | string | `0 2 * * *` | Cron schedule for running the cronjob (daily at 2 AM by default). |
| `successful_jobs_history_limit` | int | `1` | Number of successful job executions to retain in history. |
| `failed_jobs_history_limit` | int | `1` | Number of failed job executions to retain in history. |
| `mappings` | list | `[]` | Report configurations. See mapping fields below. |

## Mapping Fields

Each entry in the `mappings` list supports the following fields:

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `type` | string | yes | Finding type to report on (e.g., `finding/malware`, `finding/vulnerability`). |
| `component` | object | yes | Component specification. See component fields below. |
| `github_repository` | string | yes | GitHub repository where report is published (e.g., `github.com/org/repo`). |
| `branch` | string | `gh-pages` | Target branch for report publication. |
| `filename` | string | `report.md` | Relative path for the overview report file. |
| `dirname` | string | `reports` | Relative path for detailed per-version reports directory. |
| `auto_merge` | bool | `false` | Automatically merge the pull request that updates the report. |
| `trigger_absent_scans` | bool | `false` | Create backlog items for missing scans. |
| `report_to_saf` | bool | `false` | Upload results to SAF API. |

## Component Fields

The `component` object contains:

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `component_name` | string | yes | OCM component name to report on. |
| `ocm_repo_url` | string | no | Override default OCM repository lookup. |
| `time_range.days_from` | int | yes | Start of time range (negative = days in past). |
| `time_range.days_to` | int | yes | End of time range (0 = today). |

## Configuration Details

### GitHub Repository

The target GitHub repository where reports are published. The extension creates
a pull request with the updated report files.

**Required permissions:** The configured GitHub credentials (via `secrets.github`)
must have write access to the target repository.

### Branch Strategy

The `branch` field determines where reports are committed:

**`gh-pages` (default):** Suitable for GitHub Pages-hosted reports
```yaml
branch: gh-pages
```

**`main` or `master`:** Suitable for repositories where reports are part of documentation
```yaml
branch: main
```

**Dedicated branch:** Suitable for review workflows
```yaml
branch: reports/automated
```

### Report Structure

The extension generates two types of files:

1. **Overview report** (`filename`): Summary across all component versions
2. **Detailed reports** (`dirname/`): One file per component version with full finding details

**Example structure:**
```
reports/
├── malware-report.md              # Overview (filename)
└── malware-reports/               # Detailed reports (dirname)
    ├── 1.0.0.md
    ├── 1.1.0.md
    └── 1.2.0.md
```

### Auto Merge

When `auto_merge: true`, the extension automatically merges the pull request
that updates the report. This is useful for:
- Automated compliance dashboards
- Continuous reporting pipelines
- Read-only report repositories

**Recommendation:** Only enable for repositories dedicated to reports, not
repositories with manual review processes.

### Trigger Absent Scans

When `trigger_absent_scans: true`, the extension creates backlog items for
artefacts identified as missing scans during report generation. This ensures
comprehensive coverage.

**Use case:** Detecting gaps in scanning coverage during audit preparation
