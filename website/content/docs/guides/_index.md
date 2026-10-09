---
title: "Guides"
description: "Task-oriented guides organized by lifecycle phase: pack, sign, transport, and deploy."
icon: "🛠️"
weight: 45
aliases:
  - /docs/how-to/
  - /docs/tutorials/
sidebar:
  collapsed: true
---

Guides are task-oriented walkthroughs that get a specific job done. They are organized
by the stage of the component lifecycle you are working in:

- **Pack** — author component versions and add resources to them.
- **Sign** — generate keys, configure credentials, and sign and verify component versions.
- **Transport** — move component versions between repositories, upload artifacts, and manage
  credentials and networking along the way.
- **Deploy** — run components in a Kubernetes environment with the OCM controllers.

Alongside these phases, the **Migrate** guides help you move legacy configuration (fallback
resolvers, v1 credentials, `--upload-as` flags) to its modern equivalents as you upgrade.

If you are new to OCM, start with [Getting Started]({{< relref "docs/getting-started/_index.md" >}})
to install the CLI and create your first component version. For the ideas behind these tasks,
see [Concepts]({{< relref "docs/concepts/_index.md" >}}); for exhaustive flag and schema
details, see the [Reference]({{< relref "docs/reference/_index.md" >}}).
