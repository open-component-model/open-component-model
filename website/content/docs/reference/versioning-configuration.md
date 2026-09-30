---
title: "Versioning Configuration"
description: "Reference for OCM versioning configuration: schema, built-in scheme catalog, custom schemes, and OCI tag constraints."
icon: "🔢"
weight: 6
toc: true
---

This page is the technical reference for OCM versioning configuration. It lets OCM order, filter, and validate
component versions that do not follow semantic versioning — for example calendar versioning (CalVer) or monotonic
build numbers.

By default — when no versioning configuration is present, or when `schemes` is present but empty (`schemes: []`) — OCM
uses **loose semantic versioning** exactly as before. Once you configure at least one scheme, only the schemes you list
apply — the built-in loose-semver scheme is **not** appended automatically. To keep recognizing semver versions
alongside a custom scheme, add an explicit `builtin: loose-semver` entry (typically last, so it acts as a fallback).

## Configuration File

Versioning schemes are configured in the OCM configuration file. By default, the CLI searches for configuration in the
well-known locations (for example `$HOME/.ocmconfig`). You can also specify a configuration file explicitly with the
`--config` flag.

The versioning configuration uses the type `versioning.config.ocm.software/v1alpha1` inside a generic OCM configuration
type:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: versioning.config.ocm.software/v1alpha1
    schemes:
      # Use a built-in scheme (see the catalog below) …
      - builtin: calver-full
      # … or write a custom pattern for anything not covered:
      #   - name: release-train
      #     pattern: '^(?P<train>\d{4}Q[1-4])\.(?P<hotfix>\d+)$'
      #     comparisonGroups: [train, hotfix]
      # Keep recognizing semver versions too (optional; omit for calver-only).
      - builtin: loose-semver
```

To confirm which schemes are active for an invocation, print the effective merged configuration:

```bash
ocm get config
```

## Config Schema

| Field     | Type   | Required | Description                                          |
|-----------|--------|----------|------------------------------------------------------|
| `type`    | string | Yes      | Must be `versioning.config.ocm.software/v1alpha1`.   |
| `schemes` | array  | No       | Ordered list of version schemes. First match wins.   |

### Scheme Entry Schema

| Field              | Type   | Required          | Description                                                                                             |
|--------------------|--------|-------------------|---------------------------------------------------------------------------------------------------------|
| `name`             | string | No                | Optional label for a pattern scheme, used in diagnostics. Omit for a builtin (it has its own name).     |
| `builtin`          | string | No                | Named built-in scheme (see catalog); mutually exclusive with `pattern`/`comparisonGroups`.              |
| `pattern`          | string | Unless `builtin`  | Go (RE2) regular expression a version must match for the scheme to claim it. Use named capture groups.  |
| `comparisonGroups` | array  | No                | Named capture groups from `pattern` used to order versions, most significant first. Empty = lexical.    |

Numeric capture groups are compared as integers (so `22.10` sorts after `22.04`, and `1900` after `1838`); non-numeric
groups compare lexically.

## Built-in Scheme Catalog

Common schemes are shipped as **built-ins**: select one by name with `builtin: <name>` instead of writing a `pattern`.
A built-in is exactly the equivalent regular expression plus comparison groups — the *Equivalent regex* shown under each
entry is what the built-in resolves to, so `builtin: calver-full` and the hand-written regex behave identically.

Use a built-in for the common cases; write a custom `pattern` (see [Authoring Your Own Scheme](#authoring-your-own-scheme))
for anything not covered.

{{< tabs >}}
{{< tab "CalVer YYYY.MM.DD" >}}

```yaml
- builtin: calver-full
```

Matches `2024.03.15`. Orders by year, then month, then day.

Equivalent regex:

```yaml
- name: calver
  pattern: '^(?P<year>\d{4})\.(?P<month>\d{2})\.(?P<day>\d{2})$'
  comparisonGroups: [year, month, day]
```

{{< /tab >}}
{{< tab "CalVer YYYY.MM" >}}

```yaml
- builtin: calver-month
```

Matches `2024.03`. Orders by year, then month.

Equivalent regex:

```yaml
- name: calver-month
  pattern: '^(?P<year>\d{4})\.(?P<month>\d{2})$'
  comparisonGroups: [year, month]
```

{{< /tab >}}
{{< tab "Ubuntu YY.MM" >}}

```yaml
- builtin: calver-ubuntu
```

Matches `22.04`, `23.10`. Month compares numerically, so `22.10` sorts after `22.04`.

Equivalent regex:

```yaml
- name: ubuntu
  pattern: '^(?P<year>\d{2})\.(?P<month>\d{2})$'
  comparisonGroups: [year, month]
```

{{< /tab >}}
{{< tab "CalVer YYYY.MM.PATCH" >}}

```yaml
- builtin: calver-micro
```

Matches `2024.4.1`. The month accepts one or two digits.

Equivalent regex:

```yaml
- name: calver-micro
  pattern: '^(?P<year>\d{4})\.(?P<month>\d{1,2})\.(?P<patch>\d+)$'
  comparisonGroups: [year, month, patch]
```

{{< /tab >}}
{{< tab "AWS date YYYY-MM-DD" >}}

```yaml
- builtin: aws-date
```

Matches `2024-03-15`. Orders by year, then month, then day.

Equivalent regex:

```yaml
- name: aws-date
  pattern: '^(?P<year>\d{4})-(?P<month>\d{2})-(?P<day>\d{2})$'
  comparisonGroups: [year, month, day]
```

{{< /tab >}}
{{< tab "Build number" >}}

```yaml
- builtin: build-number
```

Matches `1837`, `1838`. Compared as integers, so `1900` sorts after `1838`.

Equivalent regex:

```yaml
- name: build
  pattern: '^(?P<build>\d+)$'
  comparisonGroups: [build]
```

{{< /tab >}}
{{< tab "Loose semver" >}}

```yaml
- builtin: loose-semver
```

The historical default (via `github.com/Masterminds/semver/v3`). Matches `1.2.3`, `v2.0.0`, `1.0.0-rc.1`. Unlike the
other built-ins it is not backed by a single regex; add it (typically last) to keep recognizing semver versions
alongside custom schemes.

{{< /tab >}}
{{< /tabs >}}

A `builtin` entry takes no `name`, `pattern`, or `comparisonGroups`: the built-in supplies its own name and behavior.
Setting any of them alongside `builtin` is rejected. The optional `name` field applies only to custom `pattern` schemes,
where it labels the scheme in diagnostics.

## Authoring Your Own Scheme

Authoring a scheme is a single configuration entry — no plugin and no build step:

1. Give the scheme a `name`.
2. Write a Go (RE2) `pattern` with **named capture groups** for the fields that determine ordering.
3. List those group names in `comparisonGroups`, **most significant first**.

Example — a "release train + hotfix" scheme where `2024Q3.2` orders by train (`2024Q3`) then hotfix (`2`):

```yaml
- name: release-train
  pattern: '^(?P<train>\d{4}Q[1-4])\.(?P<hotfix>\d+)$'
  comparisonGroups: [train, hotfix]
```

## Comparison and Ordering

Each version is resolved to the **first** scheme (in list order) that claims it — its scheme *rank*. Versions with
different ranks are ordered by rank, so entries of a higher-priority scheme sort ahead of lower-priority ones. Versions
sharing a rank are ordered by that scheme. Versions no configured scheme claims share a single "unknown" rank and are
ordered lexically among themselves, and sort after every claimed version. This rank-based order is total and
deterministic regardless of input order.

If you have not opted the built-in semver scheme back in (`builtin: loose-semver`), semver versions are unclaimed and
therefore fall into that lexical, lowest-priority "unknown" bucket — add the fallback entry if you want them ordered
properly.

Within a scheme, versions are ordered by their `comparisonGroups` left to right; numeric groups are compared as
integers and non-numeric groups lexically. With no comparison groups, the whole matched string is compared lexically.

## Version Constraints

Version *constraints* (for example `>=1.0.0 <2.0.0`, used by resolver `versionConstraint` and CLI `--constraint`) are
interpreted by the **version's resolved scheme**, not globally as semver:

- **Loose-semver** versions use full semver range syntax: `>=1.0.0 <2.0.0`, `^1.2`, `~1.2.0`.
- **Custom (regex)** schemes use **relational operators** — `>=`, `>`, `<=`, `<`, `=`, `!=` — over the scheme's own
  ordering. Each operand must itself match the scheme's `pattern`; terms are AND-composed by spaces or commas. The
  `^`, `~`, and x-range operators are semver-only and are **not** available for custom schemes, as they have no
  scheme-independent meaning.

A constraint expressed in a foreign grammar (for example a semver range applied to a calver history) is *not applicable*
to that scheme: such versions are **retained** by listing (`get`/`transfer`) and treated as **not matching** by a
resolver gate (`versionConstraint`). A version no configured scheme claims is likewise retained by listing and rejected
by gating. This means a constraint never silently discards an unrecognized history.

For example, with the CalVer scheme above, `versionConstraint: '>=2024.03.15 <2024.10.01'` keeps only CalVer versions in
that half-open window: `>=2024.03.15` matches `2024.03.15` and later, `<2024.10.01` excludes `2024.10.01` and later. A
build-number scheme (`^(?P<build>\d+)$`) with `versionConstraint: '>=1000'` keeps builds `1000`, `1837`, `12000` —
compared numerically, so `12000 >= 1000` holds even though `12000` is lexically smaller than `1000`.

A constraint that is malformed **in its own grammar** — for example an unparseable semver range like `>= not a version` evaluated against a semver version — is rejected with an error. This is distinct from the *foreign grammar* case above: a well-formed semver range applied to a calver history is not an error, it is simply not applicable (the calver versions are retained).

## OCI Tag Constraints

For OCI registries, a component version is stored as an **OCI tag**. OCI tags follow the grammar
`^[\w][\w.-]{0,127}$`: they may contain only alphanumerics, `_`, `.`, and `-`, must start with an alphanumeric or
underscore, and may be at most 128 characters.

Most calendar and build-number schemes are already valid tags (`2024.03.15`, `22.04`, `2024-03-15`, `1837`). Choose a
scheme whose versions satisfy the tag grammar. Versions containing characters such as `+`, `:`, `/`, `~`, or spaces are
not valid tags:

- A `+` (semver build metadata) is rewritten to `.build-` for the tag. This rewrite is not reversed when listing, so the
  version read back differs from the original — avoid `+` in versions destined for OCI registries.
- Any other invalid character causes `ocm add componentversion` to fail with a clear error at publish time, instead of a
  later opaque registry rejection.

CTF archives use the same reference format, so the same tag grammar applies.

## Validation

Each scheme's `pattern` is compiled when the configuration is loaded. A malformed pattern fails configuration loading
with an error naming the offending scheme and index. Every name in `comparisonGroups` must be a named capture group in
the pattern; otherwise loading fails with an error naming the missing group. A scheme entry must set exactly one of
`pattern` or `builtin`; setting both, or an unknown `builtin` value, fails configuration loading.

## Default Behavior

Without a versioning configuration, OCM uses loose semantic versioning for every component, resource, source, and
reference version — identical to previous behavior. Non-semver versions are rejected on write unless a matching scheme
is configured. Once you configure schemes, semver versions are also rejected unless you keep them with a
`builtin: loose-semver` entry, because the fallback is no longer added automatically.

An explicit empty list (`schemes: []`) behaves exactly like omitting the configuration entirely: loose semantic
versioning for everything. The `builtin: loose-semver` fallback is required only once you list at least one custom
scheme.

## Related Documentation

- [Configure a Versioning Scheme Tutorial]({{< relref "docs/tutorials/configure-versioning.md" >}}) — Hands-on
  walkthrough for calendar versioning.
- [Resolver Configuration]({{< relref "docs/reference/resolver-configuration.md" >}}) — The `versionConstraint` field
  interplays with versioning schemes.
