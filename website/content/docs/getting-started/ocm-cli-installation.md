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
wget -qO- https://ocm.software/install-cli.sh | bash
```

{{< /tab >}}
{{< tab "curl" >}}

```shell
curl -sfL https://ocm.software/install-cli.sh | bash
```

{{< /tab >}}
{{< tab "Build from Source" >}}

{{< callout title="Note" icon="outline/info-circle" >}}
Building from source is not officially supported. Use the pre-built binaries via wget or curl instead.
{{< /callout >}}

### Prerequisites

- [Git](https://git-scm.com/)
- [Go](https://go.dev/)

### Clone and build

Build the OCM CLI from the `open-component-model/open-component-model` monorepo.

```shell
git clone https://github.com/open-component-model/open-component-model.git
cd open-component-model
task cli:build   # builds to cli/tmp/bin/ocm
task cli:install # installs to /usr/local/bin (requires sudo)
```

{{< /tab >}}
{{< /tabs >}}

The binary is installed to `~/.local/bin` by default (per the [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir/latest/)).
The installer verifies binary integrity via [GitHub attestations](https://docs.github.com/en/actions/security-for-github-actions/using-artifact-attestations/using-artifact-attestations-to-establish-provenance-for-builds) when the [GitHub CLI (`gh`)](https://cli.github.com/) is available.
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

## Verify Installation

After installing, verify the CLI is working:

```shell
ocm version
```

Expected output:

```text
{"major":"0","minor":"1","patch":"0","gitVersion":"0.1.0","goVersion":"go1.26.0","compiler":"gc","platform":"darwin/arm64"}
```

## CLI Reference

For detailed command documentation, see the [OCM CLI Reference]({{< relref "/docs/reference/ocm-cli/_index.md" >}}).

## Next Steps

- [Tutorial: Create component versions]({{< relref "create-component-version.md" >}}) - Learn how to create and store component versions using the OCM CLI
