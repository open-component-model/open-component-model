# Contributing to OCM

This document helps you get started with contributing to the
[open-component-model](https://github.com/open-component-model/open-component-model) mono-repo.

For the general contribution process (fork-and-pull workflow, commit requirements, code of conduct, and more), see the
[central contributing guide](https://ocm.software/community/contributing/) on the project website.

## Prerequisites

- **Go 1.26+**
- **[Task](https://taskfile.dev/)** - runs all build, test, and lint commands
- **Docker** - required for integration tests and container builds

## Project Structure

```text
.
├── bindings/go/          # Go library modules
├── cli/                  # OCM CLI
├── kubernetes/controller # Kubernetes controllers and Helm chart
├── website/              # Project website (ocm.software)
├── conformance/          # End-to-end conformance scenarios
├── docs/
│   ├── adr/              # Architecture Decision Records
│   ├── community/        # Community and SIG docs
│   └── steering/         # Governance
├── Taskfile.yml          # Root build automation
├── golangci.yml          # Shared linter configuration
└── .env                  # Shared tool versions
```

Each area has its own contributing guide with area-specific setup, testing conventions, and development workflows:

| Area | Guide | Summary |
|------|-------|---------|
| Go library | [`bindings/go/CONTRIBUTING.md`](bindings/go/CONTRIBUTING.md) | Module structure, testify conventions, integration tests |
| CLI | [`cli/CONTRIBUTING.md`](cli/CONTRIBUTING.md) | Building, testing, documentation generation |
| Kubernetes controller | [`kubernetes/controller/CONTRIBUTING.md`](kubernetes/controller/CONTRIBUTING.md) | Ginkgo tests, envtest, CRD generation, Helm chart |
| Website | [`website/CONTRIBUTING.md`](website/CONTRIBUTING.md) | Local dev setup, Diataxis framework, content templates |

## Common Tasks

All build automation is managed through [Task](https://taskfile.dev/). Run commands from the repository root:

```bash
# List all available tasks
task --list

# Build CLI and controller
task

# Run all unit tests across every module
task test

# Run all integration tests (requires Docker)
task test/integration

# Run code generators (types, JSON schemas, deepcopy, CRDs, CLI docs)
task generate

# Lint all Go modules
task tools:lint

# Lint with auto-fix
task tools:lint -- --fix

# Lint all Markdown files
task tools:markdownlint

# Initialize the Go workspace (first time setup)
task init/go.work

# Run go mod tidy on all modules
task tidy
```

## Linting

A single `golangci.yml` at the repository root configures linting for all Go modules. The `task tools:lint` command
runs `golangci-lint` concurrently across every module using this shared configuration. Always use the task command
rather than running `golangci-lint` directly to ensure you use the correct version and config.

## Code Generation

Several types of code are generated from source:

| Generator | Task | What it produces |
|-----------|------|------------------|
| ocmtypegen | `bindings/go/generator:ocmtypegen/generate` | OCM type system code |
| jsonschemagen | `bindings/go/generator:jsonschemagen/generate` | JSON schema definitions |
| deepcopy-gen | `tools:deepcopy-gen/generate-deepcopy` | Kubernetes-style DeepCopy methods |
| controller-gen | `kubernetes/controller:manifests` | CRD, RBAC, and webhook manifests |
| controller-gen | `kubernetes/controller:generate` | Go deepcopy and runtime.Object implementations |
| CLI docs | `cli:generate/docs` | CLI reference documentation |

Run `task generate` to execute all generators at once. If you change types, schemas, CRDs, or CLI commands, run this
before committing.

## Architecture Decisions

Design decisions are documented in [`docs/adr/`](docs/adr). If you are proposing a significant change, consider writing
an ADR first.

## Questions?

- Check existing issues in the [project](https://github.com/open-component-model/ocm-project/issues) or
  [repository](https://github.com/open-component-model/open-component-model/issues)
- See [how to engage](https://ocm.software/community/) with the community
- Review the [NeoNephos Code of Conduct](https://github.com/neonephos/.github/blob/main/CODE_OF_CONDUCT.md)
