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
CI since they are all part of the same module, including cli and controller. Fix all call sites in the same PR.

Mark breaking changes by adding `!` to the PR title (e.g., `feat!: rename Foo to Bar`) so CI applies the
`!BREAKING-CHANGE!` label.

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

Integration tests need external systems: Docker for registries and storage via
[testcontainers](https://golang.testcontainers.org/), GnuPG, the GitHub API, or a sigstore scaffolding cluster. They
live in `<area>/integration/` packages.

Integration packages are grouped: each **group** is a package list (`INTEGRATION_GROUP_<name>`, listed in
`INTEGRATION_GROUPS`) in [`Taskfile.yml`](Taskfile.yml). Every package in a group is excluded from the unit test task
and runs with `test/integration`; besides the `<area>/integration` packages this includes `examples`, whose OCI step
needs Docker. CI runs one job per group and provisions what that group needs (the `sigstore` group gets the
scaffolding cluster). `task bindings/go:test/integration:groups:verify` fails when an `<area>/integration` package is
in no group or a package is in several; CI runs it before the integration jobs.

```bash
# Run one area, or one test
cd bindings/go && go test ./oci/integration/... -run Test_Integration_OCIRepository

# Run one group, or every group, through Task (adds coverage)
task bindings/go:test/integration GROUP=containers
task test/integration

# Sigstore group: provisions the scaffolding cluster first
task bindings/go/sigstore/integration:test/integration
```

A test declares the systems it needs with `testenv.Require(t, testenv.Docker, ...)` from
[`internal/testenv`](internal/testenv). A missing system skips the test with a hint on how to provide it. Run with `-v`
and look for `SKIP` to see what skipped and why.

### Conventions

For testing conventions (table-driven tests, `require.New(t)`, `t.Context()`, naming), see the testing section in the
[coding patterns guide](../../docs/coding-patterns.md).

Specific to the Go bindings: a test that needs an external system goes into an `<area>/integration/` package, calls
`testenv.Require` before using the system, and its package goes into the group whose CI job provides that system. Name
integration tests `Test_Integration_<Subject>`.

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
2. Add a `doc.go` with a package comment. Its first sentence is used as the package purpose in the
   [package table](README.md#packages).
3. Add `depguard` rules in `golangci.yml` to enforce which sibling packages may be imported.
4. Run `task generate` to regenerate the documentation tables derived from the depguard rules:
   the package table in `bindings/go/README.md` and the dependency layers in
   [`docs/dependency-table.md`](../../docs/dependency-table.md). CI verifies that both are in sync
   (`task tools:dependency-list/verify`).

## Releasing

The bindings ship together with the CLI and the controller in lockstep. One run of the
[Release](../../.github/workflows/release.yml) workflow produces three GPG-signed tags on the same commit:
`v<major>.<minor>.<patch>` (the GitHub release), `bindings/go/v<major>.<minor>.<patch>` (the Go module tag), and
`website/v<major>.<minor>.<patch>` (the versioned docs tag).

Releases are cut from `releases/vX.Y` branches by the release responsible: first a release candidate, then promotion
to the final version on the same commit. The changelog is generated from conventional commits. For the full process,
see the [release process](../../RELEASE_PROCESS.md).
