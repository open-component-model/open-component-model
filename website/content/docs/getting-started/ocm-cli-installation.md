---
title: "Install the OCM CLI"
description: "Learn how to install the OCM CLI on various platforms."
icon: "💻"
weight: 21
toc: true
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
{"major":"0","minor":"1","patch":"0","gitVersion":"0.1.0","goVersion":"go1.26.0","compiler":"gc","platform":"darwin/arm64"}
```

{{< callout title="If the output looks different" icon="outline/info-circle" >}}
If the field names are **capitalised** (`Major`, `Minor`, `Patch`), you are running the legacy v1
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

- [Tutorial: Create component versions]({{< relref "create-component-version.md" >}}) - Learn how to create and store component versions using the OCM CLI
