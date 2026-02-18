# ADR 0014: Conformance Test Suite Design

* **Status**: proposed
* **Deciders**: Maintainers
* **Date**: 2026-02-04

**Technical Story**: We need a robust conformance test suite to ensure our 
tooling (CLI and Kubernetes controllers) behaves as expected and adheres to 
a standard specification. This suite should be inspired by the Kubernetes 
conformance test suite but adapted to be more lightweight and integrated 
with our workflow.

## Context and Problem Statement

We are a platform engineering team developing tooling including a CLI and a 
Kubernetes controller. We need to ensure that released versions of our 
tooling are compatible with each other. Therefore, we need to establish a 
conformance test suite to validate our components.

Currently, our `kubernetes/controller` component has end-to-end (e2e) tests 
located in `kubernetes/controller/test/e2e`. 
These tests use [Ginkgo](https://onsi.github.io/ginkgo/) and [Gomega](https://onsi.github.io/gomega/).

However, we face several challenges and requirements:
1.  **Conformance Definition**: We need to clearly define which tests 
    constitute our "conformance profile".
2.  **Versioning**: We need to version our conformance tests to track 
    stability and compatibility over time.
3.  **Promotion Workflow**: We need a clear path to promote standard e2e
    tests to conformance tests.
4.  **Tooling Complexity**: Kubernetes uses an elaborate Ginkgo setup with 
    complex labeling for conformance. We need to weigh the added 
    functionality of complex tooling against the additional complexity.
5. **Environment Independence**: Tests should be runnable against a configurable
   environment (e.g., different versions of the ocm cli, different oci 
   registries (ghcr.io, gcr.io, docker.io, ...), different kubernetes 
   clusters (kind, gardener, ...)).

## Decision Drivers

*   **Simplicity**: Preference for standard Go tooling over complex 
    frameworks where possible.
*   **Maintainability**: Easy to write, read, and debug tests.
*   **Discoverability**: Easy to identify which tests are conformance tests.
*   **Automation**: Easy to run specific subsets of tests (e.g., only 
    conformance tests) in CI/CD.

## Considered Options

Standalone e2e Module with:

*   **Option 1**: Ginkgo/Gomega with Labels (Kubernetes style)
    *   *Description*: Continue using Ginkgo and 
        adopt its Label feature to mark tests as `[Conformance]`. Use 
        Ginkgo's CLI to filter tests.
*   **Option 2**: Standard Go Testing + Testify with
    * **Option 2.1**: Name-Based Labeling (Simple)
      * *Description* Use test function names (e.g.,
      `TestConformance_...`) to identify conformance tests and standard Go
      test filtering (`-run regexp`).
    * **Option 2.2**: Functional Labeling (Go style)
      * *Description*: Use `if strings.Contains(os.Getenv("OCM_TEST_LABEL"), 
      labels) { t.Skip() }` in test code to indentify conformance 
      tests (see https://github.com/golang/go/blob/master/src/internal/testenv/testenv.go).

## Decision Outcome

Chosen **Option 2.2**: "Standalone e2e Module with Standard Go Testing + 
Testify with Functional Labeling (Go style)".

**Justification**:
*   **Lightweight**: Avoids the "DSL" overhead of Ginkgo. Our CLI is already
    entirely based on standard Go testing with testify. In the past, our 
    developers struggled with Ginkgo's implicit behaviors and its IDE integration.
*   **Functionality**: Functional labeling allows for a similar level of 
    flexibility as Ginkgo labels while being explicit.
*   **Native Tooling**: Leveraging `go test -run` / `go test -c` is standard and
    well-understood by Go developers.
*   **Clear Separation**: A dedicated `e2e` module avoids circular 
    dependencies and keeps the test suite distinct from the component code.

### Option 2.2: Standalone e2e Module with Standard Go Testing + Testify with Functional Labeling (Go style)

#### Description

We will introduce a new top-level `e2e` Go module in the repository. 
This module will contain our end-to-end tests, a subset of which will 
serve as conformance tests.

**Key Setup Design Decisions:**
1.  **New `e2e` Module**: A dedicated place for e2e and conformance tests.
2.  **Framework**: Migration to `stretchr/testify` for assertions and standard `testing.T`.
3.  **Identification**: All tests MUST initialize a 
    `type TestMeta struct { Labels map[string]string }`. Conformance 
    tests MUST initialize this struct with a label `test-kind: conformance`.
    Before running any tests, a global variable of `type TestEnv struct { 
    Labels map[string]string }` that contains information about the test 
    environment.
4. **Configuration**: The test environment is configured by passing a command 
   line argument `./e2e.test -- --config=<config_file_path>` to the test 
   binary. The config is supposed to be YAML or JSON. The configuration file 
   can be validated against a schema to ensure correctness and documentation.
5. **Promotion**: A test is promoted from a standard e2e test to a 
    conformance test by adding a label of `test-kind: conformance` to the 
    `TestMeta` struct. This is done via Pull Request.
6. **Versioning**: The conformance tests are versioned alongside CLI and k8s 
   controllers.
7. * **Reference Scenarios**: The reference scenarios as specified in the 
   [ADR 0013](0013_reference_scenarios.md) will constitute the core of our
   conformance tests. Since the reference scenarios are supposed to show the
   commands as used by users, they will be implemented in taskfiles. The 
   taskfiles will be executed in the e2e tests.

**Technical Details:**
* **Test Containers**: [Testcontainers](https://golang.testcontainers.org/) will
  be used to run our ocm binary and other external dependencies such as oci 
  registries in isolated containers to ensure environment consistency. 
* **Providers**: We will try to implement provider interfaces for external 
  dependencies such as oci registries (e.g. distribution, zot, ghcr.io, ...) 
  and kubernetes clusters (e.g., kind, gardener, ...). This will be an 
  abstraction layer that allows us to run the same tests against different 
  oci registry or Kubernetes clusters.

## Pros and Cons of the Options

### [Option 1] Ginkgo with Labels

Pros:
*   Rich BDD style (Given/When/Then).
*   Built-in support for labeling and filtering tests.
*   Powerful tooling (generators, parallel execution).
*   Familiarity for those coming from Kubernetes development.

Cons:
*   Steep learning curve for the DSL.
*   Implicit behavior (global state, complex bootstrapping).
*   Filtering requires specific Ginkgo CLI flags or label expressions.

### [Option 2] Standard Go + Testify (Chosen)

Pros:
*   Standard Go testing patterns (no DSL to learn).
*   `stretchr/testify` provides familiar assertions.
*   Explicit filtering.
*   Common tooling for CLI and Controllers.
*   Preferred by our developers.

Cons:
*   Must enforce conventions manually (though linters can help).
*   Custom implementation for labeling and filtering instead of 
    a well-established framework with thorough documentation.

## Discovery and Distribution

*   The decision will be implemented by creating the `e2e` folder and migrating initial tests.
*   CI pipelines will be updated to run the e2e test suite.
*   Documentation will be added to `CONTRIBUTING.md` regarding how to write and promote tests.
