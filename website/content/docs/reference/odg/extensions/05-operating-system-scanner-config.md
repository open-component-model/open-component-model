---
title: "Operating System Scanner"
description: "Configuration reference for the Operating System Scanner extension."
weight: 6
toc: true
---

The Operating System Scanner extension identifies base image operating system versions and checks them against end-of-life (EOL) databases to create findings for outdated or unsupported OS versions.

## Configuration Example

```yaml
osid:
  interval: 86400                    # 24 hours
  on_unsupported: warning
```

## Top-Level Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `interval` | int (seconds) | `86400` | Maximum time before a component is re-scanned. |
| `on_unsupported` | string | `warning` | Behaviour when artefact kind/type/access is unsupported. Options: `fail`, `ignore`, `warning`. |

## Configuration Details

### `interval`

The maximum time (in seconds) before a component's artefacts are re-analysed for
operating system version information. Default is 86400 seconds (24 hours).

This interval ensures that:
- New EOL data is regularly checked against existing components
- Components are re-evaluated as operating systems reach end-of-life
- Base image changes are detected and analysed

### `on_unsupported`

Defines the behaviour when an artefact kind, type, or access method is not supported
by the OSID scanner:

- **`fail`**: Raise an exception and stop processing
- **`ignore`**: Silently skip the unsupported artefact
- **`warning`** (default): Skip the artefact and log a warning message
