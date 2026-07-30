---
title: "Install the OCM CLI"
description: "Learn how to install the OCM CLI on various platforms."
icon: "💻"
weight: 22
toc: true
---

## Overview

You can install the latest release of the OCM CLI from any of the following sources (more details below):

- [Homebrew](https://brew.sh)
- [Nix](https://nixos.org)
- [AUR](https://aur.archlinux.org/packages/ocm-cli)
- [Docker](https://www.docker.com/)
- [Podman](https://podman.io/)
- [GitHub Releases](https://github.com/open-component-model/ocm/releases)

## Bash

To install with `bash` for macOS or Linux, execute the following command:

```sh
curl -s https://ocm.software/install.sh | sudo bash
```

## Install using Homebrew

```sh
# Homebrew (macOS and Linux)
brew install open-component-model/tap/ocm
```

{{<callout context="note" title="One-time migration for users on older releases" icon="outline/refresh-cw">}}
Earlier releases of the tap installed the OCM CLI as a version-pinned formula
(e.g. `ocm@0.43.0`), which caused each release to accumulate as a separate
keg instead of upgrading in place. The tap has been fixed to use the
canonical, unversioned `ocm` formula, but Homebrew tracks installed packages
by the formula they were installed as — so existing version-pinned installs
need a one-time migration to switch onto the canonical formula.

**Symptom you may see first:**

```text
$ brew upgrade open-component-model/tap/ocm
Error: open-component-model/tap/ocm not installed
```

This is expected. From Homebrew's perspective you have `ocm@<version>`
installed, not `ocm` — they are two different formulas, even though they
ship the same binary. `brew upgrade` only upgrades formulas you already have,
so the canonical `ocm` is skipped and the existing `ocm@<version>` keg has
nothing newer to upgrade to. Run the migration below to switch over.

**Quick path** — installs the canonical formula and retargets the `ocm`
symlink in one go (older `ocm@X.Y.Z` kegs stay on disk; clean them up with
the next step or later at your leisure):

```sh
brew install open-component-model/tap/ocm
brew link --overwrite ocm
```

**Full cleanup** — remove every version-pinned keg as well:

```sh
# list every version-pinned keg you currently have installed
brew list | grep '^ocm@'

# uninstall each one (repeat for every entry above)
brew uninstall ocm@<version>   # e.g. brew uninstall ocm@0.43.0
```

After migrating, `brew upgrade` replaces the binary in place on every release.
{{</callout>}}

## Install using Nix (with Flakes)

```sh
# Nix (macOS, Linux, and Windows)
# ad hoc cmd execution
nix run github:open-component-model/ocm -- --help
nix run github:open-component-model/ocm#helminstaller -- --help

# install development version
nix profile install github:open-component-model/ocm
# or release <version>
nix profile install github:open-component-model/ocm/<version>

#check installation
nix profile list | grep ocm

# optionally, open a new shell and verify that cmd completion works
ocm --help
```

see: [Flakes](https://nixos.wiki/wiki/Flakes)

## Install from AUR (Arch Linux User Repository)

[package-url](https://aur.archlinux.org/packages/ocm-cli)

```shell
# if not using a helper util
git clone https://aur.archlinux.org/ocm-cli.git
cd ocm-cli
makepkg -i
```

[AUR Documentation](https://wiki.archlinux.org/title/Arch_User_Repository)

## Install using Docker / Podman

```sh
podman run -t ghcr.io/open-component-model/ocm:latest --help
```

### Build and Run It Yourself

```sh
podman build -t ocm .
podman run --rm -t ocm --loglevel debug --help
```

or interactively:

```sh
podman run --rm -it ocm /bin/sh
```

You can pass in the following arguments to override the predefined defaults:

- `GO_VERSION`: The **golang** version to be used for compiling.
- `ALPINE_VERSION`: The **alpine** version to be used as the base image.
- `GO_PROXY`: Your **go** proxy to be used for fetching dependencies.

Please check [hub.docker.com](https://hub.docker.com/_/golang/tags?page=1&name=alpine) for possible version combinations.

```sh
podman build -t ocm --build-arg GO_VERSION=1.22 --build-arg ALPINE_VERSION=3.19 --build-arg GO_PROXY=https://proxy.golang.org .
```

## on MS Windows

### using Chocolatey

```powershell
choco install ocm-cli
```

see: [chocolatey community package: ocm-cli](https://community.chocolatey.org/packages/ocm-cli)

### using winget

_Deprecated_: Please note, winget packages are no longer provided. Any existing packages are still working, but no new
packages are built and published to winget repository.

## Building from Source

### Prerequisites

- [git](https://www.git-scm.com/)
- [golang](https://go.dev/)
- make

### Installation Process

Clone the `open-component-model/ocm` repo:

```bash
git clone https://github.com/open-component-model/ocm
```

Enter the repository directory (`cd ocm/`) and install the cli using `make`:

```bash
make install
```

> Please note that the OCM CLI is installed in your `go/bin` directory, so you might need to add this directory to your `PATH`.

Verify the installation:

```bash
ocm version
```
