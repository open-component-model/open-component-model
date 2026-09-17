# Contributing to the OCM CLI

This guide covers development on the OCM command-line interface in `bindings/go/cli/`. For the general contribution
process, see the [central contributing guide](https://ocm.software/community/contributing/).

## Overview

The CLI is a thin [Cobra](https://github.com/spf13/cobra) command layer on top of the Go bindings in `bindings/go/`.
It provides user-facing commands for working with OCM component versions, repositories, and plugins. The architecture
has three layers:

- **Command layer** (`cmd/`) - Cobra commands that parse flags, validate input, and call into the bindings.
- **Context layer** (`internal/context/`) - A shared context that wires together configuration, the plugin manager,
  and credential resolution before any command runs.
- **Binding layer** - All OCM business logic, provided by the sibling packages of the same `bindings/go` module.
  Commands import these packages directly.

## Command Structure

Each command lives in its own package under `cmd/`. The package exports a single `New()` function that returns a
`*cobra.Command`. The entry point is `main.go`, which calls into the root command defined in `cmd/cmd.go`.

```text
cli/
├── main.go              # Entry point
├── cmd/
│   ├── cmd.go           # Root command, registers top-level commands
│   ├── get/             # Parent command grouping subcommands
│   ├── version/         # Simple leaf command
│   ├── setup/hooks/     # PersistentPreRunE bootstrap
│   └── ...
```

There are two kinds of commands:

- **Parent commands** (e.g., `get`) group subcommands. Their `RunE` returns `cmd.Help()` and they register children via
  `cmd.AddCommand()`. See `cmd/get/cmd.go` for a minimal example.
- **Leaf commands** contain the actual logic. See `cmd/version/version.go` for a simple single-command example.

## Bootstrap and Context

Before any command runs, the root command's `PersistentPreRunE` hook (`cmd/setup/hooks/pre_run.go`) bootstraps the
shared context. Because it is `Persistent`, Cobra propagates it to every subcommand. The bootstrap sequence is:

1. **Logging** - Configure `slog` from `--log-level` / `--log-format` flags.
2. **OCM config** - Load and merge configuration from standard search paths (`$OCM_CONFIG`, `~/.config/ocm/config`,
   etc.). See `cmd/configuration/ocm_config.go` for the full search order.
3. **Filesystem config** - Set up temporary folder and working directory paths.
4. **Plugin manager** - Initialize the plugin system: register built-in plugins, discover external plugins from the
   plugin directory.
5. **Credential graph** - Build the credential resolution graph from configuration.
6. **Context registration** - Store the assembled context in `cmd.Context()`.

After bootstrap, any command retrieves the context via:

```go
ocmctx := context.FromContext(cmd.Context())
ocmctx.PluginManager()      // Plugin system
ocmctx.Configuration()      // OCM config
ocmctx.CredentialGraph()    // Credential resolution
ocmctx.FilesystemConfig()   // Filesystem paths
ocmctx.SubsystemRegistry()  // Type introspection
```

The context struct and its accessors live in `internal/context/context.go`.

> [!NOTE]
> The `add component-version` command overrides `PersistentPreRunE` to inject working-directory resolution
> from the component constructor path before calling the shared bootstrap. This is the only command that customizes
> the bootstrap - all others inherit the root hook directly.

## Plugin System

The plugin manager (`bindings/go/plugin/manager/`) is the central integration point for OCM extensibility. For a
conceptual overview of how plugins work, see the
[Plugin System](https://ocm.software/docs/concepts/plugin-system/) page on the project website.

From a contributor's perspective, the key points are:

- The manager organizes plugins into typed registries - one for each capability. For the current list of registries,
  see the `PluginManager` struct in `bindings/go/plugin/manager/manager.go`.
- **Built-in plugins** are compiled into the CLI and registered at startup in `internal/plugin/builtin/builtin.go`.
- **External plugins** are discovered from the plugin directory (default `~/.config/ocm/plugins`, overridable with
  `--plugin-directory` or via OCM config).
- Commands interact with plugins through the manager's registries, never directly with plugin implementations. This
  allows the same command code to work with both built-in and external plugins transparently.

## How to Add a New Command

Each command lives in its own package and exports a `New()` function returning a `*cobra.Command`. See
`cmd/version/version.go` for a leaf command and `cmd/get/cmd.go` for a parent command. Register your command in
`cmd/cmd.go` via `cmd.AddCommand()`. For general Cobra patterns, see the
[Cobra documentation](https://github.com/spf13/cobra).

New commands automatically inherit the [bootstrap context](#bootstrap-and-context) through `PersistentPreRunE`, so
plugins, configuration, and credentials are available via `context.FromContext(cmd.Context())` without additional setup.

After adding or modifying commands, regenerate the CLI reference docs:

```bash
task bindings/go/cli:generate/docs
```

## Coding Patterns

The project's [coding patterns guide](../../../docs/coding-patterns.md) covers conventions used across the codebase. The
CLI-specific section covers:

- **Command construction** - `New()` pattern, parent/child wiring.
- **Dependency injection** - Context-based access to the plugin manager, config, and credentials at the command layer.
- **Custom flag types** - Enum and file flags with validation at set-time.
- **Output formatting** - Pluggable renderer system (JSON, YAML, NDJSON, Tree, Table) with static and live modes.

The general sections on constructors, error handling, concurrency, and the runtime type system apply equally to CLI code.

## Building

```bash
# Build for your current platform
task bindings/go/cli:build

# Binary is at bindings/go/cli/tmp/bin/ocm
./bindings/go/cli/tmp/bin/ocm version

# Install to /usr/local/bin (interactive, asks for confirmation)
task bindings/go/cli:install
```

The build embeds version information via `-ldflags`. The version defaults to a timestamp and short commit hash unless
`VERSION` is set explicitly.

## Testing

```bash
# Unit tests (skips functions with "Integration" in the name)
task bindings/go/cli:test

# Integration tests (requires Docker)
task bindings/go/cli:test/integration
```

Integration tests exercise end-to-end workflows (transfer, signing, plugin registry) against real OCI registries spun
up via [testcontainers](https://golang.testcontainers.org/). They live in `integration/`.

For testing conventions (table-driven tests, `require.New(t)`, `t.Context()`, naming), see the testing section in the
[coding patterns guide](../../../docs/coding-patterns.md).

## Relationship to Go Bindings

The CLI is part of the same Go module as the bindings (`ocm.software/open-component-model/bindings/go`). Commands
import binding packages (for example `ocm.software/open-component-model/bindings/go/runtime`) directly, so changes in
a binding package are immediately visible to the CLI and are compiled together in CI.

If your change to a public binding API breaks the CLI, fix the CLI call sites in the same PR. See the
[Go bindings contributing guide](../CONTRIBUTING.md#breaking-api-changes) for the breaking-change workflow.
