---
title: "One Module, One Release: CLI and Controller Join the Bindings"
description: "The OCM CLI and Kubernetes controller move into the monolithic bindings module — one tag, one release, same artifacts."
date: 2026-09-21T10:00:00+02:00
contributors: []
tags: ["ocm", "release", "go", "cli", "kubernetes", "developer-experience"]
draft: true
---

The OCM CLI and the OCM Kubernetes controller have moved into the monolithic Go
module at `ocm.software/open-component-model/bindings/go`. This completes the
consolidation that we started with the
[monolithic bindings]({{< relref "2026-08-14-monolithic-bindings.md" >}}).

*This change does not affect released artifacts.* If you consume the CLI
binaries, container images, or the Helm chart, nothing changes for you. 

In this post we explain what changed, why, and what it means for consumers and
contributors.

## The Problem

The CLI and the controller were top-level Go modules with their own `go.mod`
files, release cycles, and CI pipelines. A new feature often needed a change in
a binding library plus a follow-up change in the CLI or controller. Each such
change required two releases: first the binding, then the consumer.

We already removed significant friction within the bindings development,
making similar improvements to the CLI and controller is the logical next
step.

## What Changed

The directories moved into the module:

* `cli/` → `bindings/go/cli/`
* `kubernetes/controller/` → `bindings/go/kubernetes/controller/`

Starting with v0.16.0 there is a single release with a single canonical tag
(`vX.Y.Z`). The module tag `bindings/go/vX.Y.Z` is created alongside it so Go
tooling resolves the module as before. Bindings, CLI, and controller will now
always be on the same version.

Nothing changes for consumers of the bindings (other than a new minor version).
For consumers of CLI or controller as libraries, the module and import paths
have to be adjusted. The `go.mod` requirement points at the unified module:

```diff
-require ocm.software/open-component-model/cli v0.15.0
+require ocm.software/open-component-model/bindings/go v0.16.0
```

The import paths in Go sources move below the unified module:

```diff
-import "ocm.software/open-component-model/cli/..."
+import "ocm.software/open-component-model/bindings/go/cli/..."
```

Since the CLI and controller are now part of the bindings, they share the
bindings installation methods: `go get ocm.software/open-component-model/bindings/go@v0.16.0`

## What Stays the Same

* **Released artifacts**: Container images keep their names
  (`ghcr.io/open-component-model/cli`, the controller images, and the Helm
  chart under `ghcr.io/open-component-model/kubernetes/controller/chart`).
  OCM component names and CLI release binaries on GitHub Releases are
  unchanged. 
* **Dependency footprint**: `depguard` rules enforce the same boundaries that
  the separate modules used to enforce structurally.

## For Contributors

* Cross-cutting changes across the bindings, the CLI, and the controller land
  in a single PR.
* One CI pipeline tests the whole module. `go test ./...` from `bindings/go/`
  covers libraries, CLI, and controller together.

Previously released versions remain available, but new features and fixes
will only be published under the unified release.

## Background

The release strategy behind this change is documented in
[ADR 25: Bindings CI and Release Strategy](https://github.com/open-component-model/open-component-model/blob/9ae81694fd824e43562c48aa2c82eb8b6c71d3ba/docs/adr/0025_bindings_ci_and_release_strategy.md).
