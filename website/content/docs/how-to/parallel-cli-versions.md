---
title: "Run multiple OCM CLI versions in parallel"
description: "Install and switch between specific OCM CLI versions for testing and migration."
icon: "🔀"
weight: 15
toc: true
---

## Goal

Install multiple OCM CLI versions side-by-side and switch between them, useful for testing against a release candidate, validating a migration, or reproducing version-specific behavior.

## You'll end up with

- Multiple versioned OCM CLI binaries (e.g. `ocm-v0.11.0`, `ocm-v0.12.0`) on your PATH
- Optionally, a per-project version pin using `direnv`

**Estimated time:** ~10 minutes

## Prerequisites

Either:

- An [OCM CLI already installed]({{< relref "ocm-cli-installation.md" >}}), or
- Docker (to bootstrap without an existing installation; see the [Docker bootstrap]({{< relref "ocm-cli-installation.md" >}}#docker-bootstrap) section in the install guide)

And:

- `~/.local/bin` on your PATH (the default location from the [install guide]({{< relref "ocm-cli-installation.md" >}}))
- [direnv](https://direnv.net) if you want per-project version pinning (optional)

## Download a specific version

OCM distributes its own CLI as an OCM component. Any published version can be fetched directly, with no package manager or version switcher tool needed.

`--output` is required here: without it the binary is saved as `ocm` (or `ocm.exe` on Windows), overwriting your current installation. Always include the full versioned filename in the output path.

{{< tabs "download-methods" >}}
{{< tab "Existing OCM CLI" >}}

Use your current `ocm` installation to download any version.
Replace `os` and `architecture` with your platform values (`linux/darwin`, `amd64/arm64`):

```shell
ocm download resource \
  ghcr.io/open-component-model//ocm.software/cli:v0.12.0 \
  --identity os=linux,architecture=amd64 \
  --output ~/.local/bin/ocm-v0.12.0
chmod +x ~/.local/bin/ocm-v0.12.0
```

Repeat for each version you need, substituting the tag and output filename.

{{< /tab >}}
{{< tab "Docker (Linux/macOS)" >}}

Use the container image as a one-time downloader on Linux or macOS.
Replace `os` and `architecture` with your platform values (`linux/darwin`, `amd64/arm64`):

```shell
docker run --rm \
  -v "$HOME/.local/bin:/workspace" \
  -w /workspace \
  ghcr.io/open-component-model/cli:v0.12.0 \
  download resource \
  ghcr.io/open-component-model//ocm.software/cli:v0.12.0 \
  --identity os=linux,architecture=amd64 \
  --output /workspace/ocm-v0.12.0
chmod +x ~/.local/bin/ocm-v0.12.0
```

{{< /tab >}}
{{< /tabs >}}

{{< callout title="Windows" icon="outline/info-circle" >}}
Include `.exe` in the output filename. Without it the binary won't be runnable:

```powershell
ocm download resource `
  ghcr.io/open-component-model//ocm.software/cli:v0.12.0 `
  --identity os=windows,architecture=amd64 `
  --output "$HOME\.local\bin\ocm-v0.12.0.exe"
```
{{< /callout >}}

{{< callout title="Release candidates" icon="outline/info-circle" >}}
RC tags follow the same scheme: `v0.13.0-rc.1`, `v0.13.0-rc.2`, etc. Substitute the RC tag for any stable version tag above.
{{< /callout >}}

## Verify

```shell
ocm-v0.12.0 version
ocm-v0.11.0 version
```

Each command should print its own version JSON, confirming both binaries are independent and functional.

## Pin a version to a project directory

To use a specific OCM version inside one directory without affecting anything else on your system, use [direnv](https://direnv.net).

Create a local `ocm` symlink pointing at the versioned binary, then tell direnv to prepend it to PATH:

```shell
mkdir -p .bin
ln -sf ~/.local/bin/ocm-v0.12.0 .bin/ocm
echo 'PATH_add .bin' >> .envrc
direnv allow
```

When you enter the directory, `ocm` resolves to `v0.12.0`. Outside it, your default `ocm` is unchanged.

Verify the pin is active:

```shell
which ocm    # should print .bin/ocm
ocm version  # should print v0.12.0
```

{{< callout title="Note" icon="outline/info-circle" >}}
Add `.bin/` to your `.gitignore` to avoid committing the symlink.
{{< /callout >}}

## Troubleshooting

### Symptom: `command not found: ocm-v0.12.0`

**Cause:** `~/.local/bin` is not on your PATH, or the binary was not made executable.

**Fix:**

```shell
chmod +x ~/.local/bin/ocm-v0.12.0
# Bash:
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc
# Zsh:
# echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
# source ~/.zshrc
```

### Symptom: `ocm` inside the project still resolves to the system binary

**Cause:** direnv is not installed, or `.envrc` has not been allowed.

**Fix:**

```shell
direnv allow   # run from inside the project directory
```

## Next steps

- [Install the OCM CLI]({{< relref "ocm-cli-installation.md" >}}): full installation options including attestation verification
- [Download resources from component versions]({{< relref "download-resources-from-component-versions.md" >}}): the mechanism this guide is built on
