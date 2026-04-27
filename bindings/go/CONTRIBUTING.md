# Contributing to the Go Bindings

This guide covers development on the OCM Go library in `bindings/go/`. For the general contribution process, see the
[central contributing guide](https://ocm.software/community/contributing/).

## Module Structure

The library is split into independent Go modules, each with its own `go.mod` and `Taskfile.yml`. For the full list
of modules and their purpose, see the [module table in the README](README.md#modules).

Each module can be developed and tested independently. To work on a specific module:

```bash
cd bindings/go/oci
task test
```

Or run from the repository root:

```bash
task bindings/go/oci:test
```

## Go Workspace

This repository uses a [Go workspace](https://go.dev/doc/tutorial/workspaces) to manage cross-module dependencies. If
you have not set it up yet, run from the repository root:

```bash
task init/go.work
```

This creates a `go.work` file that links all modules, enabling IDE navigation and cross-module refactoring. Local
changes in one module are immediately visible to all other modules in the workspace.

**Always run `task test` from the repository root** before submitting a PR. This runs tests across all modules and
catches breakage in dependent modules early. If your change touches a module that others import, their tests will run
against your local version automatically.

> [!NOTE]
> `go.work` is gitignored and not used in CI. CI tests each module in isolation using the versions pinned in
> its `go.mod`. This means `go.work` can mask version mismatches — your code works locally because all modules resolve
> against your working tree, but CI may fail because it resolves the last released version. Be aware of this difference
> when debugging CI failures.

### Breaking API Changes

If you change a public API in a module (e.g., `runtime`) and `task test` shows failures in dependent modules (e.g.,
`oci`, `cli`), those dependent modules will need follow-up PRs after your change is released:

1. Land the PR that changes the API in the source module. Mark it as a breaking change by adding `!` to the PR title
   (e.g., `feat!: rename Foo to Bar`) so CI applies the `!BREAKING_CHANGE!` label.
2. Release the source module so a new tag is available.
3. Create follow-up PRs for each dependent module that update `go.mod` to the new version and adapt to the API change.

You cannot update the dependent modules first because their CI would still resolve the old released version and fail.

## Testing

All modules use Go's standard `testing` package with [testify](https://github.com/stretchr/testify).

### Running Unit Tests

```bash
# Run tests for a specific module
task bindings/go/oci:test

# Run all library tests from the repository root
task test

# Run a specific test
task bindings/go/oci:test -- -run TestResourceRepository
```

### Running Integration Tests

Some modules have integration tests that require external systems (Docker for OCI registries via
[testcontainers](https://golang.testcontainers.org/)). These are separated by naming convention - test functions
containing `Integration` in their name are skipped during unit test runs and only executed during integration test runs.

```bash
# Run integration tests for a specific module
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

## Adding a New Module

1. Create a directory under `bindings/go/<module-name>/`.
2. Initialize a Go module: `go mod init ocm.software/open-component-model/bindings/go/<module-name>`.
3. Create a `Taskfile.yml` that includes the shared test runner. See `bindings/go/oci/Taskfile.yml` for an example.
4. Register the module in the root `Taskfile.yml` under `includes:`.
5. Add the module to the Go workspace: `go work use bindings/go/<module-name>`.
6. Update `bindings/go/README.md` with the new module.

## Releasing a Module

Modules are versioned and released independently using Git tags. Tags follow the pattern
`bindings/go/<module>/v<major>.<minor>.<patch>` (e.g., `bindings/go/oci/v0.0.8`).

Not all modules have release tags yet. Some modules (such as `cel`, `rsa`, `signing`, and `transfer`) are consumed by
the CLI and controller via Go pseudo-versions instead of proper releases. Internal modules (such as `generator`,
`examples`, and integration test modules) are not published at all. If you are unsure whether a module needs a release,
check if it has existing tags:

```bash
git tag --list "bindings/go/<module>/v*"
```

If this returns no results, the module has not been released yet and is consumed via pseudo-versions.

Releases are created through the
[Release Go Submodule](../../.github/workflows/release-go-submodule.yaml) workflow, which is triggered manually via
`workflow_dispatch` in the GitHub Actions UI. It accepts the following inputs:

| Input | Description | Default |
|-------|-------------|---------|
| `path` | Relative path to the module (e.g., `bindings/go/oci`) | required |
| `bump` | Version bump mode (`major`, `minor`, `patch`, `none`) | `patch` |
| `suffix` | Optional pre-release suffix (e.g., `alpha1` produces `v0.0.1-alpha1`) | - |
| `dry_run` | Preview the tag and changelog without pushing | `true` |

The workflow computes the next version from the latest existing tag for that module, generates a changelog from commits
touching the module's path, and creates an annotated Git tag. For the `helm` module specifically, the workflow also
triggers a build and publish of the Helm input plugin component.

If your change affects the public API of a published module that other modules or external consumers depend on,
coordinate with the maintainers to ensure a release is published after your PR is merged. Both the CLI and the
controller reference binding modules by version in their `go.mod` files and can only pick up your changes once a new
tag exists.
