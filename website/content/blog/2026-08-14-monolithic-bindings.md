---
title: "Go Bindings: One Module, One Release"
description: "The OCM Go bindings libraries move from 30+ independently versioned modules to a single monolithic module — simpler development, simpler consumption, same modularity."
date: 2026-08-14T10:00:00+02:00
contributors: []
tags: ["ocm", "release", "go", "bindings", "developer-experience"]
draft: false
---

The OCM Go bindings have moved from 30+ independently versioned Go modules to a
single module at `ocm.software/open-component-model/bindings/go`. 

*This change does not dilute our current modularity or dependency footprint.* If
you have questions or concerns about this change please [get in touch](/community/#get-in-touch).

In this post we'll explain what changed, why, and what it means for consumers 
and contributors.

## The Problem

Each binding package (`runtime`, `oci`, `descriptor/v2`, `helm`, …) was its own
Go module with its own `go.mod` and version tag. A change in `runtime` required 
releasing it, then bumping `go.mod` in every dependent module, layer by layer, 
multiple levels deep.

This created friction in three ways:

* **Inter-module development friction**: A cross-cutting change required
  sequential PRs and releases across the dependency chain.
* **Inter-module regressions**: Each module tested against the *last
  released* version of its siblings, not the current state of main.
* **Releasing was manual and time-consuming**: Developers had to know the
  dependency graph and release in the correct order.

## What Changed

All per-binding `go.mod` files are replaced by a single `go.mod` at
`bindings/go/`. The import path is unchanged for consumers:
```go
import "ocm.software/open-component-model/bindings/go/oci"
```

The only difference in your `go.mod` is the `require` line:

```diff
-require ocm.software/open-component-model/bindings/go/oci v0.0.49
+require ocm.software/open-component-model/bindings/go v0.0.2
```

## What Stays the Same

* **Import paths**: Every existing import path continues to work. They are now
  packages within the module rather than separate modules.
* **Dependency footprint**: A consumer importing only `descriptor/v2` still gets
  the same minimal set of transitive dependencies.
* **Internal modularity**: `depguard` rules enforce the same modularity that the
  separate `go.mod` files used to enforce structurally.

## For Contributors

* Cross-cutting changes can land in a single PR.
* `go test ./...` from `bindings/go/` tests all bindings together, no `go.work`
needed.
* One release tag (`bindings/go/v0.0.x`) covers the whole library.

## For Consumers

After the first monolithic release, update your go.mod:

* Remove all references to `ocm.software/open-component-model/bindings/go/*`
* Run `go get ocm.software/open-component-model/bindings/go@v0.0.2` 
* Run `go mod tidy`

While previously released per-module versions remain available, we discourage continuing to
use them, as new features and fixes will only be published under the monolithic version.

## Background

This decision is documented in
[ADR 25: Bindings CI and Release Strategy](https://github.com/open-component-model/open-component-model/blob/9ae81694fd824e43562c48aa2c82eb8b6c71d3ba/docs/adr/0025_bindings_ci_and_release_strategy.md).
