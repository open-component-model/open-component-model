---
title: "Delivery Database Backup"
description: "Configuration reference for the Delivery Database Backup extension."
weight: 12
toc: true
---

The Delivery Database Backup extension is a cronjob which regularly creates backups of the PostgreSQL delivery database and stores them as local blobs in an OCM component. This enables disaster recovery and migration scenarios.

## Configuration Example

```yaml
delivery_db_backup:
  schedule: '0 0 * * *'              # daily at midnight
  successful_jobs_history_limit: 1
  failed_jobs_history_limit: 1
  component_name: acme.org/odg-backups
  ocm_repo_url: https://ocm-registry.example.com
  backup_retention_count: 10
  initial_version: 0.1.0
  extra_pg_dump_args: ["--verbose"]
```

## Top-Level Options

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `schedule` | string | `0 0 * * *` | Cron schedule for running the cronjob (daily at midnight by default). |
| `successful_jobs_history_limit` | int | `1` | Number of successful job executions to retain in history. |
| `failed_jobs_history_limit` | int | `1` | Number of failed job executions to retain in history. |
| `component_name` | string | — | OCM component name to store backups (e.g., `acme.org/odg-backups`). |
| `ocm_repo_url` | string | — | OCM repository URL where backups are published. |
| `backup_retention_count` | int or null | `null` | Number of backups to retain. `null` = keep all backups. |
| `initial_version` | string | `0.1.0` | Initial SemVer version for the backup component. |
| `extra_pg_dump_args` | list | `[]` | Additional arguments passed to `pg_dump`. |

## Configuration Details

### `component_name`

The name of the OCM component used to store database backups as local blobs.
Each backup becomes a new version of this component.

**Format:** Use standard OCM component naming (e.g., `organization.domain/backup-name`)

**Example:**
```yaml
component_name: acme.org/odg-production-backups
```

### `ocm_repo_url`

The OCM repository URL where backup components are published. This must be
an OCI registry that supports OCM components.

**Required Permissions:**

- **`readwrite`**: Required for creating backups
- **`admin`**: Required if `backup_retention_count` is set (to delete old backups)

**Example:**
```yaml
ocm_repo_url: https://ghcr.io/my-org
```

**Note:** Configure the corresponding OCI registry credentials via `secrets.oci-registry`
in your ODG configuration.

### `backup_retention_count`

Controls how many backup versions to retain. When set to a value > 0, the extension
automatically deletes old backups, keeping only the most recent N backups.

**Options:**

- **`null`** (default): Keep all backups indefinitely
- **Integer > 0**: Keep only the most recent N backups

**Examples:**

- `backup_retention_count: null` - Never delete backups
- `backup_retention_count: 7` - Keep last 7 backups (e.g., one week of daily backups)
- `backup_retention_count: 30` - Keep last 30 backups

**Important:** Setting this to a non-null value requires **admin** permissions on the
OCM repository to delete old component versions.

### `initial_version`

The initial SemVer version for the backup OCM component. Upon each new backup,
the minor version is automatically bumped.

**Example version progression:**

- First backup: `0.1.0`
- Second backup: `0.2.0`
- Third backup: `0.3.0`
- And so on...

**Recommendation:** Use `0.1.0` for production and `0.0.1` for testing environments.

### `extra_pg_dump_args`

Additional command-line arguments passed to the `pg_dump` utility. Use this to
customize backup behaviour.

**Common examples:**
```yaml
# Verbose output for debugging
extra_pg_dump_args: ["--verbose"]

# Include BLOB data
extra_pg_dump_args: ["--blobs"]

# Compress backup
extra_pg_dump_args: ["--compress=9"]

# Multiple arguments
extra_pg_dump_args: ["--verbose", "--compress=9"]
```

**Available options:** See [pg_dump documentation](https://www.postgresql.org/docs/current/app-pgdump.html)
