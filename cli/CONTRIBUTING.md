# Contributing to the OCM CLI

This guide covers development on the OCM command-line interface in `cli/`. For the general contribution process, see
the [central contributing guide](https://ocm.software/community/contributing/).

## Overview

The CLI is a thin [Cobra](https://github.com/spf13/cobra) command layer on top of the Go bindings in `bindings/go/`.
It provides user-facing commands for working with OCM component versions, repositories, and plugins. The architecture
has three layers:

- **Command layer** (`cli/cmd/`) - Cobra commands that parse flags, validate input, and call into the bindings.
- **Context layer** (`cli/internal/context/`) - A shared context that wires together configuration, the plugin manager,
  and credential resolution before any command runs.
- **Binding layer** (`bindings/go/`) - All OCM business logic. Commands import binding modules directly.

## Command Structure

Each command lives in its own package under `cli/cmd/`. The package exports a single `New()` function that returns a
`*cobra.Command`. The entry point is `cli/main.go`, which calls into the root command defined in `cli/cmd/cmd.go`.

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
  `cmd.AddCommand()`. See `cli/cmd/get/cmd.go` for a minimal example.
- **Leaf commands** contain the actual logic. See `cli/cmd/version/version.go` for a simple single-command example.

## Bootstrap and Context

Before any command runs, the root command's `PersistentPreRunE` hook (`cli/cmd/setup/hooks/pre_run.go`) bootstraps the
shared context. Because it is `Persistent`, Cobra propagates it to every subcommand. The bootstrap sequence is:

1. **Logging** - Configure `slog` from `--log-level` / `--log-format` flags.
2. **OCM config** - Load and merge configuration from standard search paths (`$OCM_CONFIG`, `~/.config/ocm/config`,
   etc.). See `cli/cmd/configuration/ocm_config.go` for the full search order.
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

The context struct and its accessors live in `cli/internal/context/context.go`.

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
- **Built-in plugins** are compiled into the CLI and registered at startup in `cli/internal/plugin/builtin/builtin.go`.
- **External plugins** are discovered from the plugin directory (default `~/.config/ocm/plugins`, overridable with
  `--plugin-directory` or via OCM config).
- Commands interact with plugins through the manager's registries, never directly with plugin implementations. This
  allows the same command code to work with both built-in and external plugins transparently.

## How to Add a New Command

Each command lives in its own package and exports a `New()` function returning a `*cobra.Command`. See
`cli/cmd/version/version.go` for a leaf command and `cli/cmd/get/cmd.go` for a parent command. Register your command in
`cli/cmd/cmd.go` via `cmd.AddCommand()`. For general Cobra patterns, see the
[Cobra documentation](https://github.com/spf13/cobra).

New commands automatically inherit the [bootstrap context](#bootstrap-and-context) through `PersistentPreRunE`, so
plugins, configuration, and credentials are available via `context.FromContext(cmd.Context())` without additional setup.

After adding or modifying commands, regenerate the CLI reference docs:

```bash
task cli:generate/docs
```

## Coding Patterns

The project's [coding patterns guide](../docs/coding-patterns.md) covers conventions used across the codebase. The
CLI-specific section covers:

- **Command construction** - `New()` pattern, parent/child wiring.
- **Dependency injection** - Context-based access to the plugin manager, config, and credentials at the command layer.
- **Custom flag types** - Enum and file flags with validation at set-time.
- **Output formatting** - Pluggable renderer system (JSON, YAML, NDJSON, Tree, Table) with static and live modes.

The general sections on constructors, error handling, concurrency, and the runtime type system apply equally to CLI code.

## Building

```bash
# Build for your current platform
task cli:build

# Binary is at cli/tmp/bin/ocm
./cli/tmp/bin/ocm version

# Install to /usr/local/bin (interactive, asks for confirmation)
task cli:install
```

The build embeds version information via `-ldflags`. The version defaults to a timestamp and short commit hash unless
`VERSION` is set explicitly.

## Testing

```bash
# Unit tests (skips functions with "Integration" in the name)
task cli:test

# Integration tests (requires Docker)
task cli/integration:test/integration
```

Integration tests exercise end-to-end workflows (transfer, signing, plugin registry) against real OCI registries spun
up via [testcontainers](https://golang.testcontainers.org/). They live in `cli/integration/`.

For testing conventions (table-driven tests, `require.New(t)`, `t.Context()`, naming), see the testing section in the
[coding patterns guide](../docs/coding-patterns.md).

## Relationship to Go Bindings

The CLI imports binding modules from `bindings/go/` directly in its `go.mod`. During local development, you can
**optionally** set up a [Go workspace](https://go.dev/doc/tutorial/workspaces) so that changes in a binding module are
immediately visible to the CLI without publishing a release:

```bash
task init/go.work
```

Without `go.work`, each module resolves dependencies from the versions pinned in its `go.mod`. CI always tests without
`go.work`, so each module is tested in isolation.

If your CLI change depends on a modified binding API, the binding change must be landed and released first. Then create a
follow-up PR for the CLI that updates `go.mod` to the new binding version. See the
[Go bindings contributing guide](../bindings/go/CONTRIBUTING.md#breaking-api-changes) for the full workflow.
