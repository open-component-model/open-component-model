# Contributing to OCM

Quick guide to get you building and testing the next OCM reference library, cli and controllers.

## Prerequisites

- **Go 1.25+**
- **[Task](https://taskfile.dev/)** — our build runner
- **Docker** — for integration tests

## Setup

```bash
# Clone
git clone https://github.com/open-component-model/open-component-model.git
cd open-component-model

# Verify everything builds
task
```

## Project Structure

```text
.
├── bindings/go/     # Go library modules (see bindings/go/README.md)
├── cli/             # OCM CLI
├── kubernetes/      # Kubernetes controller, this has special setup instructions (see kubernetes/controller/README.md)
├── docs/
│   ├── adr/         # Architecture Decision Records
│   ├── community/   # Community & SIG docs
│   └── steering/    # Governance
└── Taskfile.yml     # Build automation
```

## Common Tasks

```bash
# List all available tasks
task --list

# Run all unit tests
task test

# Run integration tests (requires Docker)
task test/integration

# Run tests for a specific module
task bindings/go/oci:test

# Run code generators
task generate

# Run lint
task tools:lint

# Build CLI
task cli:build
```

## Working with Modules

This is a multi-module Go workspace. Each module in `bindings/go/` has its own:

- `go.mod`
- `Taskfile.yml` with `test`, `test/integration` (if applicable)

To work on a specific module:

```bash
cd bindings/go/oci
task test
```

## Code Style

- Run `golangci-lint` before committing (CI enforces this)
  - Convenience task to run over all modules: `task tools:lint`
  - If you want to apply auto-fixing: `task tools:lint -- --fix`
- Generated code lives alongside source — run `task generate` if you change schemas

## Test Requirements

All code changes must include appropriate tests. This is a mandatory part of the review process.

### New Features

All new features **must** include unit tests that cover the expected behaviour. PRs without tests for new functionality will not be merged.

### Bug Fixes

All bug fixes **must** include a regression test that reproduces the original bug and verifies the fix. This ensures the same issue does not resurface.

### Coverage

PRs must not decrease overall test coverage. If existing uncovered code makes this impractical, discuss it in the PR description.

### Testing Patterns by Module

| Module | Framework | Key conventions |
|---|---|---|
| `bindings/go/` and `cli/` | testify (`require`, `mock`) | Table-driven tests with `t.Run()`. Start each test with `r := require.New(t)`. Prefer `t.Context()` over `context.Background()`. |
| `kubernetes/controller/` | Ginkgo v2 + Gomega | Use `--ginkgo.focus` to run specific specs, not `-run`. |

## Pull Requests

1. Fork the repo
2. Create a feature branch
3. Make your changes
4. Run `task test` and fix any failures
5. Submit PR against `main`

CI will run linting, tests, and CodeQL analysis automatically.

## Architecture Decisions

Design decisions are documented in [`docs/adr/`](docs/adr). If you're proposing a significant change, consider writing
an ADR first.

## Questions?

- Check existing [issues](https://github.com/open-component-model/open-component-model/issues)
- See the [community docs](docs/community/) for SIGs and meetings or check out how to engage with us on
  our [website](https://ocm.software/community/engagement/)!
- Review the [NeoNephos Code of Conduct](https://github.com/neonephos/.github/blob/main/CODE_OF_CONDUCT.md)

| Variable           | Default              | Description                             |
|--------------------|----------------------|-----------------------------------------|
| `IMAGE_REGISTRY`   | `localhost:5001`     | Registry URL for pushing/pulling images |
| `IMAGE_PREFIX`     | `acme.org/sovereign` | Image name prefix/organization          |
| `PUSH_IMAGE`       | `true`               | Whether to push images to registry      |
| `VERSION`          | `1.0.0`              | Component version                       |
| `POSTGRES_VERSION` | `15`                 | PostgreSQL version to use               |
