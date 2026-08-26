# Contributing to the Go Bindings

This guide covers development on the OCM Go library in `bindings/go/`. For the general contribution process, see the
[central contributing guide](https://ocm.software/community/contributing/).

## Module Structure

The library is a single Go module (`ocm.software/open-component-model/bindings/go`) split into logical packages. For
the full list of packages and their purpose, see the [package table in the README](README.md#packages).

Modularity is enforced by `depguard` rules in `golangci.yml` — each package declares which sibling packages it may
import, matching the dependency layers from [ADR 25](../../docs/adr/0025_bindings_ci_and_release_strategy.md). To reason
about the internal dependency graph between the packages, consult the [dependency table](../../docs/dependency-table.md).

Each package can be developed and tested independently:

```bash
# Run all binding unit tests
task bindings/go:test

# Run tests for a specific package
cd bindings/go && go test ./oci/...

# Run all tests from the repository root
task test
```

## Breaking API Changes

If you change a public API in a package (e.g., `runtime`), other packages that depend on it will fail immediately in
CI since they are all part of the same module. Fix all call sites in the same PR.

Mark breaking changes by adding `!` to the PR title (e.g., `feat!: rename Foo to Bar`) so CI applies the
`!BREAKING-CHANGE!` label.

The CLI and controller depend on the published monolithic module version. They will pick up your changes after the next
release (see [Releasing](#releasing)).

**Always run `task test` from the repository root** before submitting a PR. This runs tests across all modules and
catches breakage in dependent modules early.

## Testing

All modules use Go's standard `testing` package with [testify](https://github.com/stretchr/testify).

### Running Unit Tests

```bash
# Run all binding unit tests
task bindings/go:test

# Run all library tests from the repository root
task test

# Run a specific test
cd bindings/go && go test ./oci/... -run TestResourceRepository
```

### Running Integration Tests

Some packages have integration tests that require external systems (Docker for OCI registries via
[testcontainers](https://golang.testcontainers.org/)). These are separated by naming convention - test functions
containing `Integration` in their name are skipped during unit test runs and only executed during integration test runs.

```bash
# Run integration tests for a specific package
task bindings/go/oci/integration:test/integration

# Run all integration tests
task test/integration
```

### Conventions

For testing conventions (table-driven tests, `require.New(t)`, `t.Context()`, naming), see the testing section in the
[coding patterns guide](../../docs/coding-patterns.md).

The one convention specific to the Go bindings is the `Integration` naming filter: integration test functions must
include `Integration` in their name (e.g., `Test_Integration_OCIRepository`). This is how the Taskfile skip/run
patterns separate unit and integration test runs.

## Code Generation

Some modules generate code. Always run generators after changing types or schemas:

```bash
# Run all generators
task generate

# Run specific generators
task bindings/go/generator:ocmtypegen/generate
task bindings/go/generator:jsonschemagen/generate
task tools:deepcopy-gen/generate-deepcopy
```

Generated files follow the naming convention `zz_generated.deepcopy.go`.

## Adding a New Package

1. Create a directory under `bindings/go/<package-name>/`.
2. Add `depguard` rules in `golangci.yml` to enforce which sibling packages may be imported.
3. Update `bindings/go/README.md` with the new package.

## Releasing

The library is released as a single unit using the
[Release Go Bindings](../../.github/workflows/release-go-bindings.yaml) workflow, which is triggered manually via
`workflow_dispatch` in the GitHub Actions UI.

The workflow computes the next version from the latest existing tag, generates a changelog from commits
touching `bindings/go/`, and creates an annotated Git tag at `bindings/go/v<major>.<minor>.<patch>`.

If your change affects the public API of a published package that external consumers depend on,
coordinate with the maintainers to ensure a release is published after your PR is merged. Both the CLI and the
controller reference the bindings module by version in their `go.mod` files and can only pick up your changes once a new
tag exists.
