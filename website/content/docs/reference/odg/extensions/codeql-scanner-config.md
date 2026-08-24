---
title: "CodeQL Scanner"
description: "The CodeQL Scanner extension checks whether GitHub Advanced Security (GHAS) CodeQL is actively scanning each OCM source artefact. For every language present…"
weight: 180
toc: true
---
The CodeQL Scanner extension checks whether [GitHub Advanced Security (GHAS)
CodeQL](https://docs.github.com/en/code-security/code-scanning) is actively
scanning each OCM source artefact. For every language present in a repository
that is not covered by an active CodeQL analysis, the extension emits a
`finding/codeql` finding — including the case where CodeQL is not enabled at
all. Languages that CodeQL does not support (e.g. `yaml`, `dockerfile`,
`shell`) can be excluded via the `languages` parameter to suppress false
positives.

## Configuration Example

```yaml
codeql:
  interval: 86400                    # 24 hours
  on_unsupported: warning
  languages:  # exclude list — languages to skip (e.g. unsupported by CodeQL)
    - yaml
    - dockerfile
    - shell
```

## Top-Level Options

| Option | Type | Default | Description |
| -------- | ------ | --------- | ------------- |
| `interval` | int (seconds) | `86400` | Maximum time before a component is re-checked. |
| `on_unsupported` | string | `warning` | Behaviour when artefact kind/type/access is unsupported. Options: `fail`, `ignore`, `warning`. |
| `languages` | list[str] | `[]` | Languages to **exclude** from CodeQL coverage checks, using GitHub repository language names (e.g. `yaml`, `dockerfile`). Empty list (default) means findings are created for every language present in the repository that is not actively scanned by CodeQL. Non-empty list skips the listed languages entirely. |

## Configuration Details

### `languages`

An exclude list of programming languages. By default (empty list), the
extension creates a finding for every language present in the repository that
is not actively scanned by CodeQL. If non-empty, the listed languages are
skipped regardless of their CodeQL status — no finding is created for them.

This is useful to suppress findings for languages CodeQL does not support,
such as `yaml`, `dockerfile`, or `shell`. For example, with the configuration
`languages: [yaml, dockerfile]`, findings are only created for actual
programming languages like `go` or `python` — not for config/script files.

Language names must match what the GitHub repository languages API returns.
See: <https://docs.github.com/en/rest/repos/repos#list-repository-languages>

The extension automatically handles CodeQL's combined language identifiers:
`javascript-typescript` covers both `javascript` and `typescript`, and
`c-cpp` covers `c` and `c++`. Operators can simply configure `javascript`
or `c` without needing to know CodeQL's internal identifiers.

### `interval`

The maximum time (in seconds) before a component is re-checked. Default is
86400 seconds (24 hours).

### `on_unsupported`

Defines the behaviour when an unsupported artefact kind, type, or access method is encountered:

- **`fail`**: Raise an exception and stop processing
- **`ignore`**: Silently skip the unsupported artefact
- **`warning`** (default): Skip the artefact and log a warning message
