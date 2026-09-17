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

By default, and when no versioning configuration is present, OCM uses **loose semantic versioning** exactly as before.
Adding a versioning configuration is purely additive: the loose-semver scheme is always kept as the final fallback, so
existing semver versions keep working.

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
      - name: calver-date
        pattern: '^(?P<year>\d{4})\.(?P<month>\d{2})\.(?P<day>\d{2})$'
        comparisonGroups: [year, month, day]
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

| Field              | Type     | Required | Description                                                                                             |
|--------------------|----------|----------|---------------------------------------------------------------------------------------------------------|
| `name`             | string   | Yes      | Stable identifier for the scheme (e.g. `calver`, `build-number`).                                       |
| `pattern`          | string   | Yes      | Go (RE2) regular expression a version must match for the scheme to claim it. Use named capture groups.  |
| `comparisonGroups` | array    | No       | Named capture groups from `pattern` used to order versions, most significant first. Empty = lexical.    |

Numeric capture groups are compared as integers (so `22.10` sorts after `22.04`, and `1900` after `1838`); non-numeric
groups compare lexically.

## Built-in Scheme Catalog

These schemes are not special-cased in code — each is just a regular expression plus comparison groups you paste into
`schemes`. Copy the ones you need.

{{< tabs >}}
{{< tab "CalVer YYYY.MM.DD" >}}

```yaml
- name: calver-full
  pattern: '^(?P<year>\d{4})\.(?P<month>\d{2})\.(?P<day>\d{2})$'
  comparisonGroups: [year, month, day]
```

Matches `2024.03.15`. Orders by year, then month, then day.

{{< /tab >}}
{{< tab "CalVer YYYY.MM" >}}

```yaml
- name: calver-month
  pattern: '^(?P<year>\d{4})\.(?P<month>\d{2})$'
  comparisonGroups: [year, month]
```

Matches `2024.03`.

{{< /tab >}}
{{< tab "Ubuntu YY.MM" >}}

```yaml
- name: calver-ubuntu
  pattern: '^(?P<year>\d{2})\.(?P<month>\d{2})$'
  comparisonGroups: [year, month]
```

Matches `22.04`, `23.10`.

{{< /tab >}}
{{< tab "CalVer YYYY.MM.PATCH" >}}

```yaml
- name: calver-micro
  pattern: '^(?P<year>\d{4})\.(?P<month>\d{1,2})\.(?P<patch>\d+)$'
  comparisonGroups: [year, month, patch]
```

Matches `2024.4.1`.

{{< /tab >}}
{{< tab "AWS date YYYY-MM-DD" >}}

```yaml
- name: aws-date
  pattern: '^(?P<year>\d{4})-(?P<month>\d{2})-(?P<day>\d{2})$'
  comparisonGroups: [year, month, day]
```

Matches `2024-03-15`.

{{< /tab >}}
{{< tab "Build number" >}}

```yaml
- name: build-number
  pattern: '^(?P<build>\d+)$'
  comparisonGroups: [build]
```

Matches `1837`, `1838`. Compared as integers.

{{< /tab >}}
{{< /tabs >}}

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

For any pair of versions, OCM selects the **first** scheme (in list order) whose `pattern` matches **both** versions and
uses it to compare them. If no single scheme claims both, OCM falls back to the built-in loose-semver scheme and finally
to lexical comparison, so ordering is always deterministic.

Within a scheme, versions are ordered by their `comparisonGroups` left to right; numeric groups are compared as
integers and non-numeric groups lexically. With no comparison groups, the whole matched string is compared lexically.

## Version Constraints

Version *constraints* (for example `>=1.0.0 <2.0.0`, used by resolver `versionConstraint`) are a **semver** concept.
A constraint is applied only to versions whose resolved scheme is loose semver; versions claimed by a non-semver scheme
(such as calver) do not satisfy a semver constraint's notion of ordering and are retained unchanged. This means a semver
constraint filters the semver-versioned entries and never silently discards a non-semver history. A malformed constraint
string is rejected with an error.

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
the pattern; otherwise loading fails with an error naming the missing group.

## Default Behavior

Without a versioning configuration, OCM uses loose semantic versioning for every component, resource, source, and
reference version — identical to previous behavior. Non-semver versions are rejected on write unless a matching scheme
is configured.

## Related Documentation

- [Configure a Versioning Scheme Tutorial]({{< relref "docs/tutorials/configure-versioning.md" >}}) — Hands-on
  walkthrough for calendar versioning.
- [Resolver Configuration]({{< relref "docs/reference/resolver-configuration.md" >}}) — The `versionConstraint` field
  interplays with versioning schemes.
