# Contributing to the Kubernetes Controller

This guide covers development on the OCM Kubernetes controller in `kubernetes/controller/`. For the general
contribution process, see the [central contributing guide](https://ocm.software/community/contributing/).

## Overview

The controller reconciles OCM component versions into Kubernetes clusters, enabling GitOps-style deployment of OCM
resources. For background on the architecture and design, see the
[Kubernetes Controllers](https://ocm.software/docs/concepts/kubernetes-controllers/) and
[Kubernetes Deployer](https://ocm.software/docs/concepts/kubernetes-deployer/) concept pages on the project website.

The codebase uses [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime) and is deployed via a
Helm chart. The development workflow is significantly different from the Go bindings and CLI - it uses Ginkgo for
testing, envtest for simulating a Kubernetes API server, and has its own code generation pipeline for CRDs and RBAC.

## Prerequisites

In addition to the [general prerequisites](../../CONTRIBUTING.md#prerequisites), controller development requires:

- **Docker** - for building container images and running Kind clusters
- **Helm** - for chart linting, templating, and local installs
- **kubectl** - for interacting with test clusters
- **[FluxCD CLI](https://fluxcd.io/flux/installation/)** - required for E2E tests (installs Flux into the test cluster)
- **[Kind](https://kind.sigs.k8s.io/)** - required for E2E tests

All other tools (controller-gen, kustomize, envtest, kubebuilder, helm-docs, yq) are installed automatically by the
Taskfile into `kubernetes/controller/bin/`. You do not need to install them manually. Their versions are pinned in
`kubernetes/controller/.env` and managed by Renovate.

## Building

```bash
# Build the controller binary
task kubernetes/controller:build

# Build the Docker image (host architecture)
task kubernetes/controller:docker-build

# Run the controller locally (connects to your current kubeconfig context)
task kubernetes/controller:run
```

## Running Tests

### Unit Tests (envtest)

Unit tests run against a local Kubernetes API server provided by
[envtest](https://book.kubebuilder.io/reference/envtest). The Taskfile handles downloading the correct envtest binaries
automatically.

```bash
task kubernetes/controller:test
```

This command:
1. Generates CRD manifests and Go code (if needed).
2. Downloads envtest binaries for the configured Kubernetes version.
3. Runs all tests except those in `test/e2e/`.

### End-to-End Tests (Kind)

E2E tests run against a real Kubernetes cluster using [Kind](https://kind.sigs.k8s.io/):

```bash
# Set up a local Kind cluster with the controller loaded
task kubernetes/controller:test/e2e/setup/local

# Run the E2E test suite
task kubernetes/controller:test/e2e
```

The E2E setup creates a Kind cluster, installs FluxCD and [kro](https://kro.run), loads the locally built controller
image, installs the Helm chart, and then runs the Ginkgo test suite against it. For a step-by-step guide on setting up
this environment, see the [controller environment setup](https://ocm.software/docs/getting-started/set-up-controller-environments/)
on the project website. For deploying workloads, see the
[Helm chart deployment tutorial](https://ocm.software/docs/getting-started/deploy-helm-charts/) and the
[manifest deployment guide](https://ocm.software/docs/how-to/deploy-manifests-with-deployer/).

## Test Framework

The controller uses [Ginkgo v2](https://onsi.github.io/ginkgo/) (BDD-style) with
[Gomega](https://onsi.github.io/gomega/) matchers. This is different from the Go bindings and CLI, which use testify.

### Structure

Each controller package has a `suite_test.go` that bootstraps the envtest environment:

```go
func TestControllers(t *testing.T) {
    RegisterFailHandler(Fail)
    RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
    // Sets up envtest, registers CRDs, starts the controller manager
})
```

Tests are written as Ginkgo `Describe`/`It` blocks:

```go
var _ = Describe("Repository Controller", func() {
    It("should reconcile a repository", func() {
        // test body using Gomega expectations
        Expect(k8sClient.Create(ctx, repo)).To(Succeed())
        Eventually(func(g Gomega) {
            g.Expect(k8sClient.Get(ctx, key, repo)).To(Succeed())
            g.Expect(repo.Status.Conditions).To(ContainElement(...))
        }).Should(Succeed())
    })
})
```

### Running Specific Tests

```bash
# Focus on a specific Describe/It block by regex
cd kubernetes/controller
go test ./internal/controller/deployer/ -ginkgo.focus "should reconcile"

# Run a specific suite
go test ./internal/controller/repository/ -v
```

Note: Use `-ginkgo.focus` for Ginkgo specs, not `-run` (which only matches the top-level `TestControllers` function).

## CRD and RBAC Generation

CRDs, RBAC rules, and webhook configurations are generated from
[kubebuilder markers](https://book.kubebuilder.io/reference/markers) in Go source files (e.g.,
`+kubebuilder:object:root=true`, `+kubebuilder:rbac:...`). The `controller-gen` tool reads these markers and produces
the manifests:

```bash
# Generate CRD manifests, RBAC, and webhook configs into config/
task kubernetes/controller:manifests

# Generate Go deepcopy and runtime.Object implementations
task kubernetes/controller:generate
```

Generated output lands in `config/crd/bases/` and `config/rbac/`. These must stay in sync with the Helm chart
templates - see the Helm section below.

## Helm Chart

The controller is deployed via a Helm chart in `kubernetes/controller/chart/`.

### Common Tasks

```bash
# Lint the chart
task kubernetes/controller:helm/lint

# Render templates locally (uses chart/test-values.yaml)
task kubernetes/controller:helm/template

# Generate the values JSON schema from values.yaml
task kubernetes/controller:helm/schema

# Generate chart documentation (README.md)
task kubernetes/controller:helm/docs

# Full validation (sync manifests, schema, docs, lint, and check for drift)
task kubernetes/controller:helm/validate

# Install into current cluster
task kubernetes/controller:helm/install

# Uninstall
task kubernetes/controller:helm/uninstall
```

### Keeping CRDs and Helm Chart in Sync

The Helm chart templates in `chart/templates/crd/` and `chart/templates/rbac/` are generated from the kustomize output
in `config/`. The sync process uses [kubebuilder's](https://book.kubebuilder.io/) Helm plugin
(`kubebuilder edit --plugins=helm/v2-alpha`) to produce the final chart templates. After modifying any CRD or RBAC
marker, you must sync them:

```bash
task kubernetes/controller:helm/sync-manifests
```

The `helm/validate` task checks for drift and will fail if the chart is out of sync. CI enforces this.

## Development Workflow Summary

A typical change to the controller follows this flow:

1. Modify API types in `api/v1alpha1/` or controller logic in `internal/controller/`.
2. Run `task kubernetes/controller:generate` if you changed types.
3. Run `task kubernetes/controller:manifests` if you changed CRD/RBAC markers.
4. Run `task kubernetes/controller:helm/sync-manifests` if CRDs or RBAC changed.
5. Run `task kubernetes/controller:test` to verify unit tests pass.
6. Run `task kubernetes/controller:helm/validate` to ensure the chart is consistent.
7. Set up a local Kind cluster with `task kubernetes/controller:test/e2e/setup/local` and run E2E tests with
   `task kubernetes/controller:test/e2e`.
8. For manual testing, use `task kubernetes/controller:helm/install` to deploy the controller into a cluster and
   verify your changes against real workloads.
9. For end-to-end validation across the full stack (CLI, controller, kro, FluxCD), run the conformance scenario
   from `conformance/scenarios/sovereign/` with `task run`. This exercises air-gap transfer, signing, and
   deployment in a Kind cluster.
