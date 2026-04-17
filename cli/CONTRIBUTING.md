# Contributing to the OCM CLI

This guide covers development on the OCM command-line interface in `cli/`. For the general contribution process, see
the [central contributing guide](https://ocm.software/community/contributing/).

## Overview

The CLI is a thin command layer on top of the Go bindings in `bindings/go/`. It provides user-facing commands for
working with OCM component versions, repositories, and plugins.

## Building

```bash
# Build for your current platform
task cli:build

# Binary is at cli/tmp/bin/ocm
./cli/tmp/bin/ocm version

# Install to /usr/local/bin (interactive, asks for confirmation)
task cli:install

# Build for all supported platforms (linux/darwin/windows, amd64/arm64)
task cli:build/multiarch
```

The build embeds version information via `-ldflags`. The version defaults to a timestamp and short commit hash
(`0.0.0-YYYYMMDDHHMMSS-<commit>`) unless `VERSION` is set explicitly.

## Running Tests

The CLI uses the same test framework as the Go bindings: Go's `testing` package with
[testify](https://github.com/stretchr/testify).

### Unit Tests

```bash
task cli:test
```

Unit tests skip any function whose name contains `Integration`.

### Integration Tests

Integration tests require Docker and exercise end-to-end workflows (transfer, plugin registry, downloads):

```bash
task cli/integration:test/integration
```

Integration test functions must include `Integration` in their name to be picked up by the Taskfile filter.

## Testing Conventions

Same conventions as the Go bindings:

- Table-driven tests with `t.Run()`.
- Use `r := require.New(t)` for assertions.
- Use `t.Context()` for context.
- Integration tests include `Integration` in the function name.

## CLI Documentation Generation

The CLI can generate its own reference documentation from command definitions:

```bash
# Generate Markdown docs (default)
task cli:generate/docs

# Generate Hugo-compatible docs (used by the website)
task cli:generate/docs CLI_DOCUMENTATION_MODE=hugo CLI_DOCUMENTATION_DIRECTORY=cli/docs/reference
```

If you add or modify CLI commands, run the documentation generator and include the updated docs in your PR.

## Relationship to Go Bindings

The CLI imports most modules from `bindings/go/`. When working on CLI features, you will often need to modify a binding
module first. The repository uses a Go workspace (`go.work`) so that local changes in a binding module are immediately
visible to the CLI without publishing a release. See the
[Go bindings contributing guide](../bindings/go/CONTRIBUTING.md#go-workspace) for how to set it up.

Before submitting a PR, run `task test` from the repository root. This runs tests across all modules using the
workspace, so you will see if a binding change breaks the CLI or vice versa.

Note that `go.work` is gitignored and not used in CI. CI tests each module in isolation using the versions pinned in
its `go.mod`. If your CLI change depends on a modified binding API, the binding change must be landed and released
first. Then create a follow-up PR for the CLI that updates `go.mod` to the new binding version and adapts to the API
change. The CLI PR cannot be merged before the binding release because CI would still resolve the old version and fail.

If you add a new dependency on a binding module, update `cli/go.mod`:

```bash
cd cli
go get ocm.software/open-component-model/bindings/go/<module>
go mod tidy
```

## Component Construction

The CLI is also packaged as an OCM component version for distribution. This is handled by:

```bash
# Build multi-arch binaries and OCI image, generate component constructor, create CTF
task cli:generate/ctf

# Verify the resulting CTF
task cli:verify/ctf
```

You generally do not need to run these locally unless you are working on the release or packaging pipeline.
