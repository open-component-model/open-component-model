---
title: "Install the OCM CLI"
description: "Learn how to install the OCM CLI on Linux, macOS, and Windows using package managers, the install script, GitHub Actions, or downloading the binary."
icon: "💻"
weight: 10
toc: true
aliases:
  - /docs/getting-started/run-cli-from-container-image/
  - /docs/how-to/container-image-usage/
  - /docs/how-to/how-to-use-the-ocm-cli-container-image/
---

The OCM CLI is the primary tool for creating, managing, and transferring component versions.
This guide covers installation options for different platforms.

## You'll end up with

- The OCM CLI installed and ready to use on your system
- The ability to run `ocm` commands from your terminal

## Estimated time

~5 minutes

## Install the OCM CLI

{{< tabs "installation-methods" >}}

{{< tab "wget" >}}

```shell
wget -qO- https://ocm.software/install-cli.sh | {{< site-version "env" >}}bash
```

{{< /tab >}}
{{< tab "curl" >}}

```shell
curl -sfL https://ocm.software/install-cli.sh | {{< site-version "env" >}}bash
```

{{< /tab >}}
{{< tab "GitHub Actions" >}}

In GitHub Actions workflows, use [`open-component-model/ocm-setup-action`](https://github.com/open-component-model/ocm-setup-action).
It installs the CLI for the runner's OS and architecture (Linux, macOS, Windows on x64 and arm64), verifies the
binary's SHA-256 digest and its [GitHub attestation](#verify-binary-authenticity), caches it with `actions/cache`,
and adds `ocm` to the `PATH` of subsequent steps:

```yaml
on: push

jobs:
  build:
    runs-on: ubuntu-latest
    permissions:
      contents: read
    steps:
      - uses: actions/checkout@v7
      - uses: open-component-model/ocm-setup-action@v2
{{< site-version "action-with" >}}      - run: ocm version
```

On the `main` docs the example installs the latest stable release; versioned docs pin `version` to that release.
Set `cache: false` to skip the `actions/cache` restore and save — a binary already in the runner's tool
cache is still reused when its digest matches the release asset.
The action outputs the installed `version`, the binary `path`, and `cache-hit`.
See the [action's README](https://github.com/open-component-model/ocm-setup-action#readme) for all inputs and outputs.

{{< callout context="caution" title="Do not install unverified binaries" icon="outline/alert-triangle" >}}
`verify-attestation: false` installs the CLI without checking its provenance — only set it on self-hosted
runners that lack the [GitHub CLI](https://cli.github.com/), and verify the binary by another means as
described in [Verify Binary Authenticity](#verify-binary-authenticity).
{{< /callout >}}

{{< /tab >}}
{{< tab "OCM Component" >}}

OCM distributes its own CLI as an OCM component version, the same mechanism it enables
for your software.

{{< callout title="Requires an existing OCM installation" icon="outline/info-circle" >}}
This method needs `ocm` already on your PATH. For a first install, use the `wget` or `curl`
tab instead. If you have Docker but no `ocm`, see the [Docker bootstrap](#docker-bootstrap)
section below.
{{< /callout >}}

Download the CLI binary for your platform. Replace `os` and `architecture` as needed
(`linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`, `windows/arm64`).
The binary will be downloaded to the current working directory (specify `--output` to set the full output path, including filename):

```shell
ocm download resource \
  ghcr.io/open-component-model//ocm.software/cli:{{< site-version "semver" >}} \
  --identity os=linux,architecture=amd64
```

Then make the downloaded binary executable and move it to your PATH:

```shell
chmod +x ./ocm
mkdir -p $HOME/.local/bin
mv ./ocm $HOME/.local/bin/ocm
```

{{< callout title="Windows" icon="outline/info-circle" >}}
The resource is downloaded as `ocm.exe` for Windows platform if not specified otherwise using the `--output` option.
Make sure to add the directory containing `ocm.exe` to your PATH.
{{< /callout >}}

To inspect the full component version before downloading:

```shell
ocm get cv ghcr.io/open-component-model//ocm.software/cli:{{< site-version "semver" >}} -o yaml
```

<details id="docker-bootstrap">
<summary>Docker bootstrap (no existing OCM installation)</summary>

Use the official OCM container image as a one-time downloader. The image contains the
`ocm` binary and writes the downloaded resource to its working directory, which you can
map to a local path via a volume mount:

```shell
mkdir -p "$HOME/.local/bin"
docker run --rm \
  -v "$HOME/.local/bin:/workspace" \
  -w /workspace \
  ghcr.io/open-component-model/cli:{{< site-version "semver" >}} \
  download resource \
  ghcr.io/open-component-model//ocm.software/cli:{{< site-version "semver" >}} \
  --identity os=linux,architecture=amd64
chmod +x "$HOME/.local/bin/ocm"
```

Replace `os` and `architecture` with your platform. This approach works on Linux and macOS.

</details>

{{< /tab >}}
{{< tab "Run in Container" >}}

Run the CLI straight from the official container image, no local install required. The image
`ghcr.io/open-component-model/cli` ships the `ocm` binary (plus a FIPS build of `cosign` and
`gpg`) on a minimal Garden Linux `bare-libc` base with no shell.

Mount your working directory and run any `ocm` command as the container's entrypoint:

```shell
docker run --rm \
  -v "$(pwd)":/workspace:ro \
  -w /workspace \
  ghcr.io/open-component-model/cli:{{< site-version "semver" >}} \
  version
```

Pass credentials by mounting an `.ocmconfig` from the working directory (keep the mount
writable, `:rw`, for commands that produce output such as `add cv`):

```shell
docker run --rm \
  -v "$(pwd)":/workspace \
  -w /workspace \
  ghcr.io/open-component-model/cli:{{< site-version "semver" >}} \
  get cv ghcr.io/<user>//ocm.software/<component>:1.0.0 -o yaml \
  --config .ocmconfig
```

{{< callout title="Note" icon="outline/info-circle" >}}
Pin a specific tag (for example `:{{< site-version "semver" >}}`) instead of `:latest` for
reproducible runs. To let the CLI reach registries that need your system trust store, add
`-v /etc/ssl/certs/:/etc/ssl/certs/:ro` (the certificate path may differ by OS). The image has
no shell and does not yet bundle GnuPG, so run GPG signing where `gpg` is installed.
{{< /callout >}}

{{< callout title="Note" icon="outline/info-circle" >}}
The image runs `ocm` as the non-root user `65532`. If the command needs to read files that only
your user can read (for example a Docker `config.json` with mode `0600`) or write into a mounted
directory, run the container as your own user by adding `--user "$(id -u):$(id -g)"`. For commands
that write, also remove `:ro` from the bind mount and make sure that user can write to the mounted
directory.
{{< /callout >}}

For a worked create-and-read walkthrough, see
[Create Component Versions]({{< relref "create-component-version.md" >}}) and substitute the
`docker run` invocation above for the local `ocm` command.

{{< /tab >}}
{{< tab "Build from Source" >}}

{{< callout title="Note" icon="outline/info-circle" >}}
Building from source is not officially supported. Use the pre-built binaries via wget or curl instead.
{{< /callout >}}

### Prerequisites

- [Git](https://git-scm.com/)
- [Go](https://go.dev/)
- [Task](https://taskfile.dev)

### Clone and build

Build the OCM CLI from the `open-component-model/open-component-model` monorepo.

```shell
git clone https://github.com/open-component-model/open-component-model.git
cd open-component-model
{{< site-version "branch" >}}task bindings/go/cli:build   # builds to bindings/go/cli/tmp/bin/ocm
task bindings/go/cli:install # installs to /usr/local/bin (requires sudo)
```

{{< /tab >}}
{{< /tabs >}}

The binary is installed to `~/.local/bin` by default (per the [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir/latest/)).
The installer verifies binary integrity via [GitHub attestations](https://docs.github.com/en/actions/security-for-github-actions/using-artifact-attestations/using-artifact-attestations-to-establish-provenance-for-builds) when the [GitHub CLI (`gh`)](https://cli.github.com/) is available.
Set `OCM_VERSION` to install a specific version, `OCM_BIN_DIR` to control where the binary lands (default: `~/.local/bin`), and `OCM_BIN_NAME` to install the binary under a custom name.
Run `bash -s -- --help` after the pipe to see all options.

<details>
<summary>Windows Support</summary>

The install script only supports macOS and Linux. Windows binaries can be downloaded directly from the
[GitHub releases page](https://github.com/open-component-model/open-component-model/releases).

Windows support is **best-effort** and not guaranteed. While the CLI handles Windows-specific
conventions such as drive-letter paths (e.g., `C:\path\to\archive`) and backslash path separators,
there is no dedicated Windows CI infrastructure to continuously validate these code paths.

- Windows builds are cross-compiled and checked for compilation correctness.
- Windows-specific logic (such as path detection and normalization) is tested via simulated
  OS behavior on non-Windows runners.
- There is no runtime testing on actual Windows environments in CI.
- Bugs specific to Windows runtime behavior may go undetected until reported.

If you encounter a Windows-specific issue, please report it at
[github.com/open-component-model/open-component-model/issues](https://github.com/open-component-model/open-component-model/issues).

</details>

## Install a specific version

By default the script installs the latest stable release. Set `OCM_VERSION` to pin to a specific version. Use `MAJOR.MINOR` for the latest patch on that series, or `MAJOR.MINOR.PATCH` for an exact release:

```shell
wget -qO- https://ocm.software/install-cli.sh | OCM_VERSION=0.12 bash
# or with curl:
curl -sfL https://ocm.software/install-cli.sh | OCM_VERSION=0.12 bash
```

### Side-by-side versions

Set `OCM_BIN_NAME` to install the binary under a custom name (default: `ocm`), and `OCM_BIN_DIR` to set the install directory. Combine both with a version suffix to keep multiple versions side by side in the same directory:

```shell
wget -qO- https://ocm.software/install-cli.sh | OCM_VERSION=0.12 OCM_BIN_NAME=ocm-v0.12 OCM_BIN_DIR=~/.local/bin bash
wget -qO- https://ocm.software/install-cli.sh | OCM_VERSION=0.11 OCM_BIN_NAME=ocm-v0.11 OCM_BIN_DIR=~/.local/bin bash
```

Each installs the binary under the name you give it. Run by name if `~/.local/bin` is on your `PATH`, or by full path otherwise:

```shell
# Run a specific version by name (if ~/.local/bin is on PATH)
ocm-v0.12 version

# Or by full path
~/.local/bin/ocm-v0.12 version

# Switch which version is active in your shell session
ln -sf ~/.local/bin/ocm-v0.12 ~/.local/bin/ocm
ocm version
```

This is useful for running v1 and v2 side by side during a migration, testing a release candidate, or reproducing version-specific behavior when debugging.

## Verify Installation

After installing, verify the CLI is working:

```shell
ocm version
```

Expected output:

```text
OCM CLI (Open Component Model v2)
  Version:  {{< site-version "semver" >}}
```

The same command in a workflow step (`- run: ocm version`) prints this into the job log.

Releases before `0.17.0` print a lowercase JSON object instead, for example
`{"major":"0","minor":"16","patch":"0","gitVersion":"0.16.0",…}`. That is still the v2 CLI.

{{< callout title="If the output looks different" icon="outline/info-circle" >}}
If the output is a JSON object with **capitalised** field names (`Major`, `Minor`, `Patch`) instead of the
`OCM CLI (Open Component Model v2)` header, you are running the legacy v1
CLI, not v2. A previous v1 installation is shadowing the new binary. Run `which ocm` (Linux/macOS)
or `where.exe ocm` (Windows) to see which binary is active — then either move `~/.local/bin` earlier
in your PATH or remove the v1 binary. Common v1 install locations are `/opt/homebrew/bin`
(Homebrew), `/usr/local/bin` (old install script), `~/go/bin` (built from source), and the Nix
profile store.
{{< /callout >}}

## Verify Binary Authenticity

The install script automatically verifies binaries using [GitHub attestations](https://docs.github.com/en/actions/security-for-github-actions/using-artifact-attestations/using-artifact-attestations-to-establish-provenance-for-builds) when the [GitHub CLI](https://cli.github.com/) is authenticated.
If automatic verification is unavailable, you can verify manually using one of the methods below.

{{< tabs "verification-methods" >}}

{{< tab "GitHub CLI" >}}

The simplest method. Requires the [GitHub CLI](https://cli.github.com/) with authentication.

```shell
gh auth login --hostname github.com
# Set this to the binary you installed (adjust the path if you used a custom name or directory).
binary="${HOME}/.local/bin/ocm"
gh attestation verify "$binary" --repo open-component-model/open-component-model
```

{{< /tab >}}
{{< tab "Cosign (no GitHub auth)" >}}

Uses [Sigstore cosign](https://docs.sigstore.dev/cosign/signing/overview/) to cryptographically verify the binary's provenance.
No GitHub authentication required. The attestation API is public.

```shell
# Set this to the binary you installed (adjust the path if you used a custom name or directory).
binary="${HOME}/.local/bin/ocm"

# Compute the binary's SHA-256 digest
DIGEST="sha256:$(sha256sum "$binary" | cut -d' ' -f1)"
# On macOS, use: DIGEST="sha256:$(shasum -a 256 "$binary" | cut -d' ' -f1)"

# Download the Sigstore attestation bundle from the public GitHub API
curl -sfL \
  "https://api.github.com/repos/open-component-model/open-component-model/attestations/${DIGEST}" \
  | jq -r '.attestations[0].bundle' > attestation.jsonl

# Verify with cosign
cosign verify-blob-attestation \
  --bundle attestation.jsonl \
  --new-bundle-format \
  --type slsaprovenance1 \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp \
    '^https://github\.com/open-component-model/open-component-model/\.github/workflows/cli\.yml@refs/(heads/(main|releases/v[0-9]+\.[0-9]+)|tags/cli/v[0-9]+\.[0-9]+\.[0-9]+)' \
  "$binary"
```

A successful verification proves the binary was built by the project's GitHub Actions workflow and signed via Sigstore OIDC.

{{< /tab >}}
{{< tab "Manual SHA-256" >}}

Verify integrity by comparing your binary's hash against the digests recorded in the attestation (no extra tools needed beyond `curl` and `jq`).

```shell
# Set this to the binary you installed (adjust the path if you used a custom name or directory).
binary="${HOME}/.local/bin/ocm"

# Compute the binary's SHA-256 digest
DIGEST="sha256:$(sha256sum "$binary" | cut -d' ' -f1)"
# On macOS, use: DIGEST="sha256:$(shasum -a 256 "$binary" | cut -d' ' -f1)"

# Fetch expected digests from the attestation
curl -sfL \
  "https://api.github.com/repos/open-component-model/open-component-model/attestations/${DIGEST}" \
  | jq -r '.attestations[0].bundle.dsseEnvelope.payload' \
  | base64 --decode | jq '.subject[] | "\(.digest.sha256)  \(.name)"'
```

If your binary's digest appears in the output, it matches the attested build artifact.

{{< callout title="Note" icon="outline/info-circle" >}}
This verifies integrity (the file hasn't been corrupted) but not authenticity (it could theoretically be replaced along with the attestation by an attacker who compromises GitHub infrastructure). For full cryptographic proof, use the cosign method above.
{{< /callout >}}

{{< /tab >}}
{{< /tabs >}}

## CLI Reference

For detailed command documentation, see the [OCM CLI Reference]({{< relref "/docs/reference/ocm-cli/_index.md" >}}).

## Next Steps

- [Getting Started: Create component versions]({{< relref "create-component-version.md" >}}) - Learn how to create and store component versions using the OCM CLI
