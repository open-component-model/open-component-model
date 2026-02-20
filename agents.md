# Agents Guide — Open Component Model (OCM)

This document contains accumulated knowledge about the OCM repository for any LLM or AI agent working with this codebase.

## Repository Overview

OCM is a multi-module Go monorepo implementing the Open Component Model specification. It consists of three main areas:

- **bindings/go/** — independent Go library modules (the core libraries)
- **cli/** — The `ocm` CLI tool built with Cobra
- **kubernetes/controller/** — A controller-runtime-based Kubernetes operator

All modules use **Go 1.25.5**. The build system uses **Task** (not Make). There are no Makefiles.
The task file documentation should be followed located here: https://taskfile.dev/docs/guide.

## Agent Behavior Rules

- Be concise. Use simple sentences. Technical jargon is fine.
- Do NOT overexplain basic concepts. Assume the user is technically proficient.
- Avoid flattering, corporate, or marketing language. Maintain a neutral viewpoint.
- Avoid vague or generic claims not substantiated by context.
- Do NOT add comments on lines you are adding unless the logic is non-obvious.
- In tests, always use `t.Context()` instead of `context.Background()` or `context.TODO()`.

## Code Review Rules

### No Cross-Module Pollution
- PRs must not mix changes across multiple Go modules.
- Each PR should focus on a single module unless there's a clear dependency relationship.

### Follow Package Structure and Order
- Changes must align with the architecture described in doc.go files of each module and package if it exists.
- Respect the established package hierarchy and dependencies.
- Maintain consistency with existing patterns in the package.

### Controller Performance
- Pay special attention to operations handling large numbers of objects.
- Consider: watch/list efficiency, reconciliation loop performance, memory usage, caching strategies.

## Global Conventions

### Build System

```bash
# Use task -a to understand and know what kind of tasks are available for running.
task -a
```

The root `Taskfile.yml` includes module-specific taskfiles. Each module under `bindings/go/` has its own `Taskfile.yml` that reuses `reuse.Taskfile.yml` for test tasks.

### Import Order (gci enforced)

```go
import (
    "context"
    "fmt"

    _ "embed"

    "github.com/spf13/cobra"

    "ocm.software/open-component-model/bindings/go/runtime"
)
```

Order: standard library → blank imports → dot imports → third-party → OCM modules.

### Sentinel Errors

```go
var ErrNotFound = errors.New("credentials not found")
```

### Commit Convention (enforced by CI)

```
type(scope): subject

feat(cli): add new command
fix(repository): handle nil pointer
chore(deps): update dependencies
```

Types: `feat`, `fix`, `chore`, `docs`, `test`, `perf`. Breaking changes use `!`: `feat(api)!: remove deprecated method`.

### Code Generation

Three generators exist: `ocmtypegen`, `jsonschemagen`, and `deepcopy-gen`. Use `task -a` to find generation targets. Always run generation after adding or modifying markers.

### Linting

Config: `golangci.yml` at repo root. Use `task -a` to find linting targets.

### Runtime Type System

The foundation of OCM. Every typed object has `runtime.Type` (Name + Version).

```go
type MyType struct {
    Type runtime.Type `json:"type"`
}
func (t *MyType) GetType() runtime.Type  { return t.Type }
func (t *MyType) SetType(typ runtime.Type) { t.Type = typ }
```

Types are registered in `runtime.Scheme` — a thread-safe registry mapping `Type` → `reflect.Type`. The `Typed` interface requires `GetType()`, `SetType()`, and `DeepCopyTyped()`.

### CI Pipeline

- Smart module detection: CI discovers Go modules dynamically and filters based on changed files
- Tests only run for affected modules on PRs; full suite on main
- Pipeline: conventional commit validation → auto-labeling → module discovery → lint → unit tests → integration tests → CodeQL → generation verification
- Multi-arch builds for CLI and controller (linux/darwin, amd64/arm64)

---

## Coding Patterns

For detailed coding patterns, conventions, and idiomatic Go practices used across this repository, see [docs/coding-patterns.md](docs/coding-patterns.md).

---

## Area-Specific Notes

### bindings/go/

Each module under `bindings/go/` is an independent Go module. Analyze the target module's `doc.go`, `README.md`, and existing code for structure and conventions before making changes.

- **Testing**: testify only (`require` and `mock`). No Ginkgo. Table-driven tests with `t.Run()`. Start every test with `r := require.New(t)`.
- **Test data**: Embedded via `//go:embed testdata`.

### cli/

Analyze `cli/README.md` and `cli/cmd/` for structure and conventions.

- **Testing**: testify/require. The `test.OCM()` helper in `cmd/internal/test/test.go` executes CLI commands programmatically with an options builder pattern.
- **Integration tests** live in `cli/integration/` with a separate `go.mod` and use testcontainers.

### kubernetes/controller/

Analyze `kubernetes/controller/README.md` and `kubernetes/controller/api/` for structure, CRDs, and conventions.

- **Testing**: Ginkgo v2 + Gomega. This is the only area using Ginkgo.
- **Critical env var**: `export ENVTEST_K8S_VERSION=1.34.1` — without this, tests fail with path errors.
- **Filtering Ginkgo tests**: Use `--ginkgo.focus`, not `-run`.
- **Test helpers** in `internal/test/` provide mock object builders.

## Common Pitfalls

1. **Missing ENVTEST_K8S_VERSION** — Controller tests will fail silently with path errors
2. **Cross-module PRs** — CI rejects PRs that mix changes across multiple Go modules
3. **Forgetting `task generate`** — After adding/changing markers, generated code must be committed
4. **Using `-run` with Ginkgo** — Use `--ginkgo.focus` instead
5. **Interactive git** — Don't use `-i` flags in scripts
6. **Context** — Always pass `context.Context` through APIs
7. **APIs are WIP** — Expect changes, especially in bindings

## Dependency Management

- Renovate handles updates automatically
- Auto-merge for minor/patch, manual review for major
- OCM monorepo deps update only at 22:00-06:00 UTC
- After manual updates: `go get <module>@<version> && task tidy`
- If you need to add a dependency, check online what the latest compatible version is

## Architecture Decision Records

Located in `docs/adr/`. 16 ADRs covering plugins, credentials, transfer, signing, controller migration, release strategy, plugin registry, and OCI format compatibility. Template at `docs/adr/0000_template.md`.

## Debugging

```bash
ocm --loglevel debug <command>                    # CLI debug logging
./my-plugin server --config='...' 2>&1 | tee plugin.log  # Plugin logs
ocm get componentversion <component> -o yaml      # Inspect descriptors
```
