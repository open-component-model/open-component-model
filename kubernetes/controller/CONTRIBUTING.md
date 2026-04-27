# Contributing to the Kubernetes Controller

This guide covers development on the OCM Kubernetes controller in `kubernetes/controller/`. For the general
contribution process, see the [central contributing guide](https://ocm.software/community/contributing/).

## Overview

The controller reconciles OCM component versions into Kubernetes clusters. For the full architecture, reconciliation
chain, and concept overview, see the
[Kubernetes Controllers](https://ocm.software/docs/concepts/kubernetes-controllers/) page on the project website.

From a contributor's perspective, the controller consists of four reconcilers that form a pipeline — each custom resource
depends on the previous one becoming `Ready`:

- **Repository** validates that an OCM repository is reachable at the configured interval.
- **Component** resolves a component version using semver constraints and optionally verifies its signature.
- **Resource** resolves a specific resource from the component and publishes access metadata in its status. This
  metadata is what downstream consumers (like the Deployer) use to locate and fetch the resource content.
- **Deployer** downloads resource content and applies it to the cluster (the resource must contain valid Kubernetes
  manifests). It uses the [ApplySet](internal/controller/applyset/) implementation for server-side apply and pruning.

The codebase uses [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime) and is deployed via a
Helm chart.

## Where Code Lives

```
kubernetes/controller/
├── cmd/main.go                          # Manager bootstrap, plugin registration
├── api/v1alpha1/                        # CRD type definitions (kubebuilder markers)
├── internal/
│   ├── controller/                      # Reconcilers (one per CRD above)
│   ├── ocm/                             # Base reconciler, shared OCM utilities
│   └── resolution/                      # Component resolution worker pool + cache
├── chart/                               # Helm chart
└── hack/                                # Generation scripts (CRD/RBAC)
```

All reconcilers embed a shared base reconciler from `internal/ocm/reconciler.go` that provides `ctrl.Client`,
`runtime.Scheme`, and `record.EventRecorder`.

## How OCM is Integrated

The controller uses the same OCM [plugin system](https://ocm.software/docs/concepts/plugin-system/) as the CLI. At
startup, `cmd/main.go` registers plugins for OCI component repositories, RSA signing, OCI credentials, resource
fetching, digest processing, and blob transformation. These plugins handle all communication with OCM repositories.

Component descriptor resolution runs through a worker pool with an in-memory LRU cache
(`internal/resolution/workerpool/`). The worker pool is added as a controller-runtime `Runnable` so the manager handles
its lifecycle.

## CRD and Code Generation

CRDs and RBAC rules are generated from [kubebuilder markers](https://book.kubebuilder.io/reference/markers) in the Go
source files under `api/v1alpha1/`. The generation pipeline has three steps:

1. **`task manifests`** — Runs `controller-gen` to produce raw CRD YAML into `bin/gen/crd/` and raw RBAC into
   `bin/gen/rbac/`.
2. **`task generate`** — Generates Go deepcopy and `runtime.Object` implementations from type definitions.
3. **`task helm/generate`** — Wraps the raw CRDs and RBAC from step 1 into Helm chart templates (adding conditionals,
   cert-manager annotations, etc.) via scripts in `hack/`. The output goes into `chart/templates/crd/` and
   `chart/templates/rbac/`.

To validate that everything is in sync:

```bash
task kubernetes/controller:helm/validate
```

This regenerates all artifacts, lints the chart, renders templates, and checks that the working tree is clean. CI
enforces this — if you modify `api/v1alpha1/` types or RBAC markers and forget to regenerate, CI will fail.

## Prerequisites

In addition to the [general prerequisites](../../CONTRIBUTING.md#prerequisites), controller development requires:

- **Docker** — for building container images and running Kind clusters
- **Helm** — for chart linting, templating, and local installs
- **kubectl** — for interacting with test clusters
- **[FluxCD CLI](https://fluxcd.io/flux/installation/)** — required for E2E tests
- **[Kind](https://kind.sigs.k8s.io/)** — required for E2E tests
- **[kro](https://kro.run)** — required for E2E tests

All other tools (controller-gen, kustomize, envtest, helm-docs, yq) are installed automatically by the Taskfile into
`kubernetes/controller/bin/`. Their versions are pinned in `kubernetes/controller/.env`.

## Building

```bash
# Build the controller binary
task kubernetes/controller:build

# Build the Docker image (host architecture)
task kubernetes/controller:docker-build

# Run the controller locally (connects to your current kubeconfig context)
task kubernetes/controller:run
```

## Testing

### Unit Tests (envtest)

Unit tests run against a local Kubernetes API server provided by
[envtest](https://book.kubebuilder.io/reference/envtest). The Taskfile handles downloading the correct envtest binaries.

```bash
task kubernetes/controller:test
```

### End-to-End Tests (Kind)

E2E tests run against a real Kubernetes cluster using [Kind](https://kind.sigs.k8s.io/):

```bash
# Set up a local Kind cluster with the controller loaded
task kubernetes/controller:test/e2e/setup/local

# Run the E2E test suite
task kubernetes/controller:test/e2e
```

The E2E setup creates a Kind cluster, installs FluxCD and kro, loads the locally built controller
image, and installs the Helm chart.

### Test Framework

The controller uses [Ginkgo v2](https://onsi.github.io/ginkgo/) with [Gomega](https://onsi.github.io/gomega/) matchers.
This is different from the Go bindings and CLI, which use testify. Each controller package has a `suite_test.go` that
bootstraps the envtest environment — see any of the `internal/controller/*/suite_test.go` files for the pattern.

Use `-ginkgo.focus` to run specific specs (not `-run`, which only matches the top-level test function).

For controller-specific testing patterns (reconciler structure, condition handling, resource references), see the
controller idioms section in the [coding patterns guide](../../docs/coding-patterns.md).

## Helm Chart

The chart lives in `chart/` and is the primary deployment mechanism.

```bash
# Lint the chart
task kubernetes/controller:helm/lint

# Render templates locally
task kubernetes/controller:helm/template

# Generate values JSON schema and chart docs
task kubernetes/controller:helm/schema
task kubernetes/controller:helm/docs

# Install into / uninstall from current cluster
task kubernetes/controller:helm/install
task kubernetes/controller:helm/uninstall
```

## Development Workflow Summary

A typical change to the controller follows this flow:

1. Modify API types in `api/v1alpha1/` or controller logic in `internal/controller/`.
2. Run `task kubernetes/controller:test` to verify unit tests pass (this also regenerates code and manifests).
3. Run `task kubernetes/controller:helm/validate` to ensure CRDs, RBAC, and the chart are consistent.
4. Set up a local Kind cluster with `task kubernetes/controller:test/e2e/setup/local` and run E2E tests with
   `task kubernetes/controller:test/e2e`.
