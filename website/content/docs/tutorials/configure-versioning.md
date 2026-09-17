---
title: "Configure a Versioning Scheme"
description: "Teach OCM to accept and order calendar-versioned (CalVer) component versions using a versioning configuration."
weight: 40
toc: true
---

By default OCM assumes semantic versioning. This tutorial shows how to configure a **calendar versioning** (CalVer)
scheme so OCM accepts versions like `2024.03.15` and orders them newest-first.

## What You'll Learn

By the end of this tutorial, you will:

- Write a `versioning.config.ocm.software/v1alpha1` entry in your `.ocmconfig`
- Add and list CalVer component versions with the OCM CLI
- Confirm the active versioning scheme with `ocm get config`

**Estimated time:** ~5 minutes

## Prerequisites

- [OCM CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) installed
- A working directory you can write to (example: `/tmp/ocm-versioning`)

## Scenario

- **Component:** `acme.org/service`
- **Versions:** `2024.03.15` and `2024.10.01` (CalVer `YYYY.MM.DD`)
- **Repository:** a local CTF archive `./ctf`
- **Config file:** `versioning.ocmconfig`

## Tutorial Steps

{{< steps >}}
{{< step >}}

### Write the versioning configuration

Create `versioning.ocmconfig` with a CalVer scheme. The built-in loose-semver scheme is always kept as a fallback, so
semver versions keep working.

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: versioning.config.ocm.software/v1alpha1
    schemes:
      - name: calver-date
        pattern: '^(?P<year>\d{4})\.(?P<month>\d{2})\.(?P<day>\d{2})$'
        comparisonGroups: [year, month, day]
```

{{< /step >}}

{{< step >}}

### Create a component constructor

Save this as `component-constructor.yaml`:

```yaml
components:
  - name: acme.org/service
    version: "2024.03.15"
    provider:
      name: acme.org
```

{{< /step >}}

{{< step >}}

### Add the first CalVer version

Without the versioning config this fails version validation. With `--config versioning.ocmconfig`, the CalVer scheme
accepts it:

```bash
ocm --config versioning.ocmconfig add cv --repository ./ctf --constructor component-constructor.yaml
```

{{< details "Expected output" >}}
```text
 COMPONENT         │ VERSION    │ PROVIDER
───────────────────┼────────────┼──────────
 acme.org/service  │ 2024.03.15 │ acme.org
```
{{< /details >}}

{{< /step >}}

{{< step >}}

### Add a newer CalVer version

Change `version` in `component-constructor.yaml` to `"2024.10.01"` and add it to the same archive:

```bash
ocm --config versioning.ocmconfig add cv --repository ./ctf --constructor component-constructor.yaml
```

{{< /step >}}

{{< step >}}

### List versions and observe the ordering

```bash
ocm --config versioning.ocmconfig get cv ./ctf//acme.org/service -o yaml
```

Both versions are accepted and listed newest-first: `2024.10.01` appears before `2024.03.15`. The CalVer scheme
compares the year, month, and day capture groups numerically — not lexically.

{{< /step >}}

{{< step >}}

### Confirm the active scheme

Print the effective merged configuration to verify the CalVer scheme is loaded:

```bash
ocm --config versioning.ocmconfig get config
```

The output includes your `versioning.config.ocm.software/v1alpha1` entry.

{{< /step >}}
{{< /steps >}}

## What you've learned

- You configured a regex-based CalVer scheme with named comparison groups.
- You added and listed non-semver component versions that OCM would otherwise reject.
- You confirmed correct newest-first ordering and inspected the active configuration.

## Troubleshooting

### Problem: `add cv` fails with an invalid version error

**Cause:** The version does not match any configured scheme (or you forgot `--config versioning.ocmconfig`).

**Fix:** Ensure the `pattern` matches your version string and that you pass the config file.

### Problem: The version is rejected as an invalid OCI tag

**Cause:** OCI tags allow only `^[\w][\w.-]{0,127}$`. A version containing `+`, `:`, `/`, `~`, or spaces cannot be a tag.

**Fix:** Choose a scheme whose versions are valid OCI tags. See
[OCI Tag Constraints]({{< relref "docs/reference/versioning-configuration.md" >}}).

## Related documentation

- [Versioning Configuration]({{< relref "docs/reference/versioning-configuration.md" >}}) — Full schema, scheme catalog,
  and OCI tag constraints.
- [Resolver Configuration]({{< relref "docs/reference/resolver-configuration.md" >}}) — How `versionConstraint`
  interacts with versioning schemes.
