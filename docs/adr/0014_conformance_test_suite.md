# ADR 0014: Conformance Test Suite Design

* **Status**: proposed
* **Deciders**: Maintainers
* **Date**: 2026-02-04

**Technical Story**: We need a robust conformance test suite to ensure our tooling (CLI and Kubernetes controllers) behaves as expected and adheres to a standard specification. This suite should be inspired by the Kubernetes conformance test suite but adapted to be more lightweight.

## Context and Problem Statement

We are a platform engineering team developing a CLI and a Kubernetes controller. We need to ensure that released versions of our tooling are compatible with each other and external systems.

Currently, our `kubernetes/controller` component has Ginkgo-based end-to-end (e2e) tests. We need to evolve this into a proper conformance suite with the following requirements:

1.  **Conformance Definition**: Clearly define which tests constitute our "conformance profile".
2.  **Versioning & Promotion**: Version conformance tests and provide a workflow to promote e2e tests to conformance.
3.  **Tooling Complexity**: Avoid the complexity of Kubernetes' elaborate Ginkgo setup.
4.  **Environment Independence**: Tests must run against configurable environments (various OCI registries, Kubernetes clusters).

## Decision Drivers

*   **Simplicity**: Preference for standard Go tooling.
*   **Maintainability**: Easy to write, read, and debug.
*   **Discoverability**: Clear identification of conformance tests.
*   **Automation**: Efficient filtering and execution in CI/CD.

## Considered Options

*   **Option 1**: **Ginkgo/Gomega with Labels** (Kubernetes style)
    *   *Description*: Use Ginkgo's Label feature to mark tests as `[Conformance]`.
*   **Option 2**: **Standard Go Testing + Testify**
    *   *Description*: Use standard Go testing with a custom functional labeling mechanism (e.g., `if !HasLabel("conformance") { t.Skip() }`).

## Decision Outcome

Chosen **Option 2**: "Standard Go Testing + Testify".

**Justification**:
*   **Lightweight**: Avoids Ginkgo's DSL overhead and implicit behaviors.
*   **Native Tooling**: Leveraging `go test` is standard and well-understood.
*   **Flexibility**: Functional labeling provides explicit control without framework magic.
*   **Separation**: A dedicated `e2e` module avoids circular dependencies.

### Implementation Details

We will introduce a new top-level `e2e` Go module containing all end-to-end tests, a subset of which are conformance tests.

#### Key Design Decisions

1.  **Framework**: `stretchr/testify` for assertions and standard `testing.T`.
2.  **Identification**: 
    *   Tests must initialize a `TestMeta` struct containing labels.
    *   Conformance tests are identified by the label `test-kind: conformance`.
3.  **Environment Configuration**: 
    *   Tests receive environment configuration via a command-line argument: `./e2e.test -- --config=<config.yaml>`.
    *   This config defines the OCI registry, Kubernetes cluster, etc.
4.  **Promotion**: 
    *   Promote a test by adding `test-kind: conformance` to its `TestMeta` labels via Pull Request.
5.  **Versioning**: 
    *   Conformance tests are versioned with the codebase.
6.  **Reference Scenarios**: 
    *   Core conformance tests will implement the Reference Scenarios (ADR 0013).
    *   These will largely be executed as Taskfiles invoked by the Go tests.

#### Technical Details

*   **Test Containers**: Use [Testcontainers](https://golang.testcontainers.org/) for isolated dependencies (OCM binary, OCI registries).
*   **Providers**: Implement provider interfaces for external dependencies (OCI registries, Clusters) to ensure tests are agnostic to the specific backing service.

## Pros and Cons of the Options

### Option 1: Ginkgo with Labels

*   **Pros**: Rich BDD style, built-in labeling, powerful tooling.
*   **Cons**: Steep learning curve, implicit behavior, complex bootstrapping.

### Option 2: Standard Go + Testify (Chosen)

*   **Pros**: Standard patterns, explicit filtering, simple debugging, preferred by the team.
*   **Cons**: Requires manual convention enforcement (mitigated by linters).

## Next Steps

*   Create the `e2e` folder and migrate initial tests.
*   Update CI pipelines to execute the suite.
*   Document test creation and promotion workflows in `CONTRIBUTING.md`.
*   Incrementally deprecate and migrate e2e tests to the new suite.
