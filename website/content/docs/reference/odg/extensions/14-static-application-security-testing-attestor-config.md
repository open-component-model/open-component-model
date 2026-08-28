---
title: "Static Application Security Testing Attestor"
description: "Configuration reference for the SAST Attestor extension."
weight: 15
toc: true
---

The Static Application Security Testing Attestor extension checks whether static analysis scans have been executed for components and creates findings when scans are missing or outdated.

## Configuration Example

```yaml
sast:
  interval: 86400                    # 24 hours
  on_unsupported: warning
```

## Top-Level Options

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `interval` | int (seconds) | `86400` | Maximum time before a component is re-checked. |
| `on_unsupported` | string | `warning` | Behaviour when artefact kind/type/access is unsupported. Options: `fail`, `ignore`, `warning`. |

## Configuration Details

### `interval`

The maximum time (in seconds) before a component is re-checked for SAST
scan execution. Default is 86400 seconds (24 hours).

This interval ensures:

- Components are regularly verified for SAST coverage
- New components are identified quickly if they lack scans
- Scan execution tracking remains current

### `on_unsupported`

Defines the behaviour when an artefact kind, type, or access method is not supported:

- **`fail`**: Raise an exception and stop processing
- **`ignore`**: Silently skip the unsupported artefact
- **`warning`** (default): Skip the artefact and log a warning message
