---
title: "Backlog Controller"
description: "The Backlog Controller is a controller that automatically scales worker pods based on the number of pending backlog work-items. It ensures optimal resource…"
weight: 80
toc: true
---
The Backlog Controller is a controller that automatically scales worker pods based on the number of pending backlog work-items. It ensures optimal resource utilization by dynamically adjusting the number of extension workers in response to workload.

## Configuration Example

```yaml
backlog_controller:
  max_replicas: 5
  backlog_items_per_replica: 3
  remove_claim_after_minutes: 30
```

## Top-Level Options

| Option | Type | Default | Description |
| -------- | ------ | --------- | ------------- |
| `max_replicas` | int | `5` | Maximum number of replicas per extension that the controller scales up to. |
| `backlog_items_per_replica` | int | `3` | Number of backlog work-items required before increasing replica count. |
| `remove_claim_after_minutes` | int | `30` | Time after which a claimed work-item is released if still processing. |

## Configuration Details

### `max_replicas`

The maximum number of replicas (worker pods) the controller will scale up to for each
extension. This prevents runaway scaling and ensures cluster resources are not exhausted.

**Important:** The issue-replicator extension is always limited to a maximum of 1 replica,
regardless of this setting, to prevent potential duplicate GitHub issues from concurrent processing.

**Example scenarios:**

- `max_replicas: 1` - No scaling, always single worker per extension
- `max_replicas: 5` - Allow up to 5 concurrent workers per extension
- `max_replicas: 10` - Higher concurrency for demanding workloads

### `backlog_items_per_replica`

The threshold that determines when to scale up. The controller increases replicas when:

```
number_of_pending_items / backlog_items_per_replica > current_replicas
```

**Example with `backlog_items_per_replica: 3`:**

- 1-3 items pending → 1 replica
- 4-6 items pending → 2 replicas
- 7-9 items pending → 3 replicas
- And so on, up to `max_replicas`

**Tuning guidance:**

- **Lower values** (e.g., `2`): More aggressive scaling, faster processing, higher resource usage
- **Higher values** (e.g., `5`): More conservative scaling, lower resource usage, potentially slower processing
- **Consider:** The average processing time for work-items when choosing this value

### `remove_claim_after_minutes`

When a worker claims a backlog work-item, it signals that it's processing that item.
If the claim persists longer than this duration, the controller assumes the worker
has stalled or crashed and releases the claim so another worker can process it.

**Example scenarios:**

- Worker pod crashes mid-processing
- Worker encounters an unhandled error and hangs
- Network issues prevent completion notification

**Tuning guidance:**

- **Too low** (e.g., `5`): Risk of duplicate processing if work-items legitimately take longer
- **Too high** (e.g., `120`): Stalled work-items block the queue for extended periods
- **Recommended:** Set to 2-3× the typical work-item processing time

Default of 30 minutes works well for most scanning workloads.
