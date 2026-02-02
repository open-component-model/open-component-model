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

```
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

## Pull Requests

1. Fork the repo
2. Create a feature branch
3. Make your changes
4. Run `task test` and fix any failures
5. Submit PR against `main`

CI will run linting, tests, and CodeQL analysis automatically.

## Architecture Decisions

Design decisions are documented in [`docs/adr/`](docs/adr). If you're proposing a significant change, consider writing an ADR first.

## Questions?

- Check existing [issues](https://github.com/open-component-model/open-component-model/issues)
- See the [community docs](docs/community/) for SIGs and meetings or check out how to engage with us on our [website](https://ocm.software/community/engagement/)!
- Review the [Code of Conduct](https://github.com/cncf/foundation/blob/main/code-of-conduct.md)
