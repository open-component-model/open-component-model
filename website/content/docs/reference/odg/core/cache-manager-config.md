---
title: "Cache Manager"
description: "The Cache Manager is a cronjob which regularly updates persistent cache entries and purges stale cache entries when the cache size exceeds configured…"
weight: 100
toc: true
---
The Cache Manager is a cronjob which regularly updates persistent cache entries and purges stale cache entries when the cache size exceeds configured limits. It optimises performance by pre-calculating expensive operations and intelligently managing cache eviction.

## Configuration Example

```yaml
cache_manager:
  schedule: '*/10 * * * *'             # every 10 minutes
  successful_jobs_history_limit: 1
  failed_jobs_history_limit: 1
  max_cache_size_bytes: 1000000000   # 1 GB
  min_pruning_bytes: 100000000       # 100 MB
  cache_pruning_weights:
    creation_date_weight: 0
    last_update_weight: 0
    delete_after_weight: -1.5        # stale entries -> delete
    keep_until_weight: -1            # expired entries -> delete
    last_read_weight: -1             # unused entries -> delete
    read_count_weight: 10            # frequently read -> keep
    revision_weight: 0
    costs_weight: 10                 # expensive to recalculate -> keep
    size_weight: 0
  prefill_function_caches:
    functions:
      - compliance-summary
      - component-versions
    components:
      - component_name: ocm.software/open-delivery-gear
        version: greatest
      - component_name: acme.org/my-component
        version: 1.0.0
```

## Top-Level Options

| Option | Type | Default | Description |
| -------- | ------ | --------- | ------------- |
| `schedule` | string | `*/10 * * * *` | Cron schedule for running the cronjob (every 10 minutes by default). |
| `successful_jobs_history_limit` | int | `1` | Number of successful job executions to retain in history. |
| `failed_jobs_history_limit` | int | `1` | Number of failed job executions to retain in history. |
| `max_cache_size_bytes` | int | `1000000000` | Maximum allowed cache size in bytes (1 GB default). |
| `min_pruning_bytes` | int | `100000000` | Amount of space to free when pruning (100 MB default). |
| `cache_pruning_weights` | object | — | Weights for cache eviction algorithm. See weights below. |
| `prefill_function_caches` | object | — | Configuration for pre-calculating function results. |

## Cache Pruning Weights

Each weight influences the cache eviction algorithm. Higher weights make entries less likely to be deleted:

| Weight | Type | Default | Description |
| -------- | ------ | --------- | ------------- |
| `creation_date_weight` | float | `0` | Weight based on when entry was created. |
| `last_update_weight` | float | `0` | Weight based on last update time. |
| `delete_after_weight` | float | `-1.5` | Negative value prioritizes deletion of stale entries. |
| `keep_until_weight` | float | `-1` | Negative value prioritizes deletion of expired entries. |
| `last_read_weight` | float | `-1` | Negative value prioritizes deletion of long-unused entries. |
| `read_count_weight` | float | `10` | Positive value protects frequently accessed entries. |
| `revision_weight` | float | `0` | Weight based on entry revision. |
| `costs_weight` | float | `10` | Positive value protects expensive-to-recalculate entries. |
| `size_weight` | float | `0` | Weight based on entry size. |

## Prefill Configuration

The `prefill_function_caches` object contains:

| Option | Type | Description |
|--------|------|-------------|
| `functions` | list | Functions to pre-calculate. Options: `compliance-summary`, `component-versions`. |
| `components` | list | Components for which to pre-calculate results. |

Each component in the `components` list has:

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `component_name` | string | yes | OCM component name. |
| `version` | string or object | yes | Version (`greatest` for latest, specific version, or GitHub source). |

## Configuration Details

### `prefill_function_caches`

Pre-calculates expensive operations for specified components to improve
dashboard and API response times.

#### Available Functions

**`compliance-summary`**  
Pre-calculates compliance summaries, aggregating findings across components.
This speeds up dashboard landing pages and compliance reports.

**`component-versions`**  
Pre-calculates component version lists and metadata. This accelerates
component browsing and navigation.

#### Prefill Components

Specify which components should have results pre-calculated:

```yaml
prefill_function_caches:
  functions:
    - compliance-summary
    - component-versions
  components:
    - component_name: ocm.software/open-delivery-gear
      version: greatest              # Always prefill latest version
    - component_name: acme.org/critical-component
      version: 2.1.0                 # Prefill specific version
```

**Recommendation:** Only prefill components that are:

- Frequently accessed in the dashboard
- Large enough that calculation is noticeably slow
- Part of critical workflows (compliance reporting, executive dashboards)

Prefilling too many components can increase cache manager runtime and
cache size without meaningful performance benefits.

## Monitoring Cache Health

Watch for these indicators that cache tuning is needed:

- **Frequent pruning events**: Increase `max_cache_size_bytes`
- **Slow dashboard loading**: Add components to `prefill_function_caches`
- **High cache manager runtime**: Reduce prefill components or increase `min_pruning_bytes`
- **Stale cached data**: Ensure `delete_after_weight` and `keep_until_weight` are negative
