# OCM CLI

The `ocm` command-line interface for the Open Component Model: create, sign, verify, transfer, and download component versions.

> **⚠️ Work in Progress** — Commands and flags may still change. For the legacy v1 CLI, see [ocm v1](https://github.com/open-component-model/ocm).

## Installation

### Install script (Linux/macOS)

```bash
curl -sfL https://ocm.software/install-cli.sh | bash
```

Installs the binary to `~/.local/bin` (override with `OCM_BIN_DIR`); make sure that directory is on your `PATH`, or run the binary by its full path. Set `OCM_VERSION` to choose a version: `MAJOR.MINOR` installs the latest patch of that series, `MAJOR.MINOR.PATCH` an exact release. Windows binaries are available from the [GitHub releases page](https://github.com/open-component-model/open-component-model/releases). See the full guide: [Install the OCM CLI](https://ocm.software/docs/getting-started/install-the-ocm-cli/).

### Container image

```bash
docker run --rm ghcr.io/open-component-model/cli:latest version
```

Pin a specific release tag instead of `:latest` for reproducible runs.

### From source

Requires [Git](https://git-scm.com/), [Go](https://go.dev/) (version in [`bindings/go/go.mod`](../go.mod)), and [Task](https://taskfile.dev).

```bash
git clone https://github.com/open-component-model/open-component-model.git
cd open-component-model
task bindings/go/cli:build        # binary at bindings/go/cli/tmp/bin/ocm
./bindings/go/cli/tmp/bin/ocm version
task bindings/go/cli:install      # optional: copies to /usr/local/bin (sudo, asks first)
```

## Quick Start

```bash
# Print the CLI version
ocm version

# Inspect a public component version
ocm get cv ghcr.io/open-component-model//ocm.software/cli:0.17.0 -o yaml

# Download a resource for a specific platform
ocm download resource ghcr.io/open-component-model//ocm.software/cli:0.17.0 --identity name=cli,os=linux,architecture=amd64
```

## Commands

| Command | Description |
| --- | --- |
| [`add`](docs/reference/ocm_add.md) | Add anything to OCM |
| [`completion`](docs/reference/ocm_completion.md) | Generate the autocompletion script for the specified shell |
| [`describe`](docs/reference/ocm_describe.md) | Describe OCM entities or metadata |
| [`download`](docs/reference/ocm_download.md) | Download anything from OCM |
| [`generate`](docs/reference/ocm_generate.md) | Generate documentation for the OCM CLI |
| [`get`](docs/reference/ocm_get.md) | Get anything from OCM |
| [`plugin`](docs/reference/ocm_plugin.md) | Manage OCM plugins |
| [`sign`](docs/reference/ocm_sign.md) | create signatures for component versions in OCM |
| [`transfer`](docs/reference/ocm_transfer.md) | Transfer anything in OCM |
| [`verify`](docs/reference/ocm_verify.md) | verify digests and signatures of component versions in OCM |
| [`version`](docs/reference/ocm_version.md) | Retrieve the build version of the OCM CLI |

Full reference: [docs/reference/ocm.md](docs/reference/ocm.md), also published at <https://ocm.software/docs/reference/ocm-cli/>.

## Configuration

The global `--config` flag loads one or more `.ocmconfig` or `ocm/config` files. When it is not set, the CLI searches the `OCM_CONFIG` environment variable, the XDG/home config directories, the current working directory, and the executable's directory, in that order, and merges every file it finds; later files take precedence (see the full lookup order in [docs/reference/ocm.md](docs/reference/ocm.md)). Control logging with `--loglevel debug` and `--logformat json`.

## Shell Completion

Cobra's built-in `completion` command generates completion scripts for each supported shell:

```bash
source <(ocm completion bash)                 # bash (needs the bash-completion package)
source <(ocm completion zsh)                  # zsh
ocm completion fish | source                  # fish
ocm completion powershell | Out-String | Invoke-Expression  # PowerShell
```

To load completions in every new shell, see `ocm completion <shell> --help`. Reference: [docs/reference/ocm_completion.md](docs/reference/ocm_completion.md).

## Development

See [CONTRIBUTING.md](../../../CONTRIBUTING.md). Run unit tests with `task bindings/go/cli:test` and integration tests (needs Docker; sources in [`integration/`](integration/)) with `task bindings/go/cli:test/integration`.

## See Also

- [Go Library](../README.md)
- [Architecture Decisions](../../../docs/adr/)
- [OCM Specification](https://github.com/open-component-model/ocm-spec)
- [OCM documentation](https://ocm.software)
