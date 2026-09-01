# OCM CLI

A new Command-line interface for the Open Component Model.

> **⚠️ Work in Progress** — Commands may change. For stable CLI, see [ocm v1](https://github.com/open-component-model/ocm).

## Installation

### From Source

```bash
# Clone the repo
git clone https://github.com/open-component-model/open-component-model.git
cd open-component-model

# Build
task cli:build

# Binary is at cli/tmp/bin/ocm
./cli/tmp/bin/ocm version
```

## Quick Start

```bash
# Check version
ocm version

# Get a component version from an OCI registry
ocm get cv ghcr.io/my-org/ocm//my-component:1.0.0
```

## Command Reference

Full command documentation: [docs/reference/ocm.md](docs/reference/ocm.md)

## Shell Completion

```bash
# Bash
ocm completion
```

## Plugins

The CLI supports plugins for additional repository types and capabilities.

```bash
# List available plugins
ocm plugin registry list

# Get plugin info
ocm plugin registry get <plugin-name>
```

See [ADR-0001: Plugins](../docs/adr/0001_plugins.md) for architecture details.

## See Also

- [Go Library](../bindings/go/README.md)
- [Architecture Decisions](../docs/adr/)
- [OCM Specification](https://github.com/open-component-model/ocm-spec)
