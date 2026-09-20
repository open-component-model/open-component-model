---
title: "Scaffolding Components: ocm init Gets You Started in One Command"
description: "The new ocm init command inspects a repository and drafts a component-constructor file — deterministically, with zero configuration."
date: 2026-09-20T08:00:00+02:00
contributors: []
tags: ["ocm", "cli", "developer-experience", "getting-started"]
draft: false
---

Writing your first `component-constructor.yaml` by hand means learning the OCM
resource model before you have shipped anything: which resource type is your
deliverable, which access or input describes it, what version to stamp, how a
monorepo maps to components and references. The new `ocm init` command writes
that first draft for you — and it works with **zero configuration**.

## One command, no setup

Point `ocm init` at a repository:

```console
ocm init ./my-service
```

No API key, no config file, no network round-trip required. Classification is
**deterministic**: it reads the evidence already in your checkout — detected
manifests (`go.mod`, `Chart.yaml`, `Dockerfile`, `package.json`, …), whether
there is a `cmd/` directory or a `func main`, and git signals (origin, tags,
branch) — and produces the same scaffold every time.

Run against OCM's own controller repository and you get, with no setup:

```yaml
components:
  - name: github.com/open-component-model/ocm-controller
    version: 0.33.0
    provider:
      name: github.com/open-component-model
    resources:
      - name: chart
        type: helmChart
        relation: local
        input:
          type: Helm/v1
          path: deploy
    sources:
      - name: source
        type: git
        access:
          type: GitHub/v1
          repoUrl: https://github.com/open-component-model/ocm-controller
          commit: 8b0631258c0f53b3478c0b69dd8c6852f810bdbc
```

The version `0.33.0` comes from the repository's real git tags, the provider
from its origin, and the chart path from the actual `deploy/Chart.yaml`. Feed it
straight to `ocm add cv` and it builds.

## It tells you what to do next

The point of a getting-started tool is to get you to the next step, so every run
ends with a summary and the exact command to run:

```text
Discovered a single component using deterministic rules (no model):
  • github.com/open-component-model/ocm-controller@0.33.0
      kind: helmChart · version from git-tag · provider github.com/open-component-model

Next step:
  ocm add cv --repository <ctf-or-registry> --constructor component-constructor.yaml --working-directory .
```

When the tool cannot discover a value — for example a published container image
tag that appears in no `values.yaml`, `Makefile`, goreleaser, or CI config — it
says so explicitly instead of emitting a confident-but-wrong value:

```text
Before building, verify:
  ! github.com/prometheus/prometheus: verify the image reference
    "ghcr.io/prometheus/prometheus:3.14.0" — it is a best-effort guess, not a
    discovered value
```

## Monorepos, handled conservatively

Real repositories keep build scaffolding and tooling in subdirectories. A
controller with `component/` and `deploy/` folders, or a binary with an
`internal/tools` submodule, is still **one** component — and `ocm init` keeps it
that way. It only fans out into multiple components when there are genuinely
independent sibling deliverables and no dominant root, like a chart collection:

```text
Discovered a monorepo (8 components) using deterministic rules:
  • .../charts/enterprise-logs@2.5.1     kind: helmChart · version from git-tag
  • .../charts/loki-distributed@0.80.6   kind: helmChart · version from git-tag
  ...
  • github.com/grafana/helm-charts       kind: aggregate
```

Each chart resolves its own version from the repository's tags — including the
Helm-style `name-1.2.3` tag convention — and the root aggregate references them
all.

## When you want a second opinion

For genuinely ambiguous repositories, `ocm init --ai-assist` routes the
judgemental decisions through the TypeSafe Jev decisions model. It is strictly
additive: the facts the rules already know stay authoritative, and if no API key
is configured the command falls back to the deterministic rules rather than
failing. You are never blocked on setting up an external service to get started.

## Ships what your CI ships

A repository usually publishes more than one thing — a binary *and* an image, or
a matrix of both. Beyond the primary deliverable, `ocm init` reads your release
and CI configuration — `.goreleaser.yaml`, GitHub Actions workflows,
`Makefile`/`Taskfile` targets, and `ko` — and adds each discovered output as its
own resource: container images (with tags resolved from `{{ .Version }}`
templates) and released binaries (with their platform matrix). Anything it cannot
resolve is flagged for you to verify.

## Start here, then refine

`ocm init` produces a first draft, not a finished component. It reads a local
checkout, so it cannot resolve transitive dependencies from a remote registry —
and where it has to fall back to a guess, it tells you exactly what to check.
Review the scaffold, fix the flagged fields, and build.

See the [`ocm init` reference](/docs/reference/ocm-init-classification/) for the
full classification rules, configuration, and the list of what it can and cannot
discover.
