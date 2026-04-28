---
title: "Migrate from Fallback to Deterministic Repository Resolvers"
description: "Replace deprecated fallback resolvers with glob-based resolvers for deterministic and efficient component resolution."
weight: 10
toc: true
---

## Goal

Replace the deprecated `ocm.config.ocm.software` fallback resolver configuration with the new
`resolvers.config.ocm.software/v1alpha1` glob-based
resolvers.

{{< callout context="caution" >}}
The fallback resolver (`ocm.config.ocm.software`) is deprecated. It uses priority-based ordering with prefix matching
and probes multiple
repositories until one succeeds. The glob-based resolver (`resolvers.config.ocm.software/v1alpha1`) replaces it with
first-match glob-based matching
against component names and optional semver version constraints, which is simpler and more efficient.
{{< /callout >}}

## Why Migrate?

- The fallback resolver (`ocm.config.ocm.software`) is **deprecated** and will be removed in a future release.
- The fallback resolver probes multiple repositories on every lookup, which adds latency and makes it harder to reason
  about which repository is used.
- Glob-based resolvers use first-match semantics — the outcome is determined by list order alone, making configurations
  simpler to understand and debug.

## Prerequisites

- [OCM CLI]({{< relref "/docs/getting-started/ocm-cli-installation.md" >}}) installed
- An existing `.ocmconfig` file that uses `ocm.config.ocm.software` resolver entries

## Steps

Suppose you have the following legacy resolver config in `$HOME/.ocmconfig`:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: ocm.config.ocm.software
    resolvers:
      - repository:
          type: OCIRepository/v1
          baseUrl: ghcr.io
          subPath: my-org/team-a
        prefix: my-org.example/services
        priority: 10
      - repository:
          type: OCIRepository/v1
          baseUrl: ghcr.io
          subPath: my-org/team-b
        prefix: my-org.example/libraries
        priority: 10
      - repository:
          type: CommonTransportFormat/v1
          filePath: ./local-archive
        priority: 1
```

The following steps walk you through each change needed to migrate to glob-based resolvers.

{{< steps >}}

{{< step >}}
**Change the config type from `ocm.config.ocm.software` to `resolvers.config.ocm.software/v1alpha1`**

Replace the configuration type:

```yaml
configurations:
  - type: resolvers.config.ocm.software/v1alpha1  # was: ocm.config.ocm.software
    resolvers:
      ...
```

{{< /step >}}

{{< step >}}
**Replace `prefix` with `componentNamePattern`**

The fallback resolver uses `prefix` to match component names by string prefix. The glob-based resolver uses
`componentNamePattern`,
which supports [glob patterns]({{< relref "docs/reference/resolver-configuration.md#component-name-patterns" >}}).
In most cases, appending `/*` to the old prefix is the closest equivalent. Note that the old `prefix` also matched the
bare prefix itself as an exact
component name (e.g. `prefix: my-org.example/services` matched both `my-org.example/services` and
`my-org.example/services/foo`). If you have
components that match the bare prefix, use `{,/*}` instead:

```yaml
    resolvers:
      - repository:
          type: OCIRepository/v1
          baseUrl: ghcr.io
          subPath: my-org/team-a
        # matches my-org.example/services and my-org.example/services/*
        componentNamePattern: "my-org.example/services{,/*}"  # was: prefix: my-org.example/services
```

If no component uses the bare prefix as its name (which is the common case), `/*` is sufficient:

```yaml
        componentNamePattern: "my-org.example/services/*"  # was: prefix: my-org.example/services
```

If a resolver had an empty prefix (matching all components), use `*` as the pattern:

```yaml
        componentNamePattern: "*"  # was: prefix: "" (or no prefix)
```

{{< /step >}}

{{< step >}}
**Remove the `priority` field**

Glob-based resolvers do not use priorities. Instead, resolvers are evaluated in the order they appear in the list, and
the **first match wins**.
That's one of the key differences from the fallback resolver, which tries all matching resolvers in priority order until
one succeeds.
If your legacy resolvers had equal `priority` values, keep their original list order to preserve the same resolution
behaviour (the fallback resolver uses a stable sort, so equal-priority entries were tried in insertion order).

For new configs, place more specific patterns before broader ones:

```yaml
    resolvers:
      # specific patterns first
      - repository:
          type: OCIRepository/v1
          baseUrl: ghcr.io
          subPath: my-org/team-a
        componentNamePattern: "my-org.example/services/*"
      # broader patterns last
      - repository:
          type: CommonTransportFormat/v1
          filePath: ./local-archive
        componentNamePattern: "*"
```

{{< /step >}}

{{< step >}}
**Review the final config**

Your migrated config should now look like this:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: resolvers.config.ocm.software/v1alpha1
    resolvers:
      - repository:
          type: OCIRepository/v1
          baseUrl: ghcr.io
          subPath: my-org/team-a
        componentNamePattern: "my-org.example/services/*"
      - repository:
          type: OCIRepository/v1
          baseUrl: ghcr.io
          subPath: my-org/team-b
        componentNamePattern: "my-org.example/libraries/*"
      - repository:
          type: CommonTransportFormat/v1
          filePath: ./local-archive
        componentNamePattern: "*"
```

{{< /step >}}

{{< step >}}
**Verify**

Run any OCM command that resolves components:

```bash
ocm get cv ghcr.io/my-org/team-a//my-org.example/services/my-service:1.0.0 \
  --recursive=-1 --config .ocmconfig
```

If you still see the warning `using deprecated fallback resolvers, consider switching to glob-based resolvers`, check
that you removed all
`ocm.config.ocm.software` configuration blocks.
Both resolver types can coexist in the same config file during migration — the fallback resolvers will still work but
will emit the deprecation
warning.

{{< /step >}}

{{< /steps >}}

## Migrating Version-Split Repositories

The fallback resolver had a **probe-and-retry** behaviour: it tried all matching repositories in priority order until
one
succeeded. This allowed the same component to have versions spread across multiple repositories without additional
configuration.

The glob-based resolver uses **first-match** semantics — it does not probe or retry. If the same component has versions
in different repositories, use the `versionConstraint` field to route each version range to the correct repository.

**Example:** `my-component` has older versions in `old-registry.example/legacy` and newer versions in
`new-registry.example/current`:

{{< tabs >}}
{{< tab "Fallback (before)" >}}

```yaml
- type: ocm.config.ocm.software
  resolvers:
    - repository:
        type: OCIRepository/v1
        baseUrl: new-registry.example
        subPath: current
      prefix: my-org.example
      priority: 10
    - repository:
        type: OCIRepository/v1
        baseUrl: old-registry.example
        subPath: legacy
      prefix: my-org.example
      priority: 1
```

{{< /tab >}}
{{< tab "Glob-based (after)" >}}

```yaml
- type: resolvers.config.ocm.software/v1alpha1
  resolvers:
    - repository:
        type: OCIRepository/v1
        baseUrl: new-registry.example
        subPath: current
      componentNamePattern: "my-org.example/*"
      versionConstraint: ">=2.0.0"
    - repository:
        type: OCIRepository/v1
        baseUrl: old-registry.example
        subPath: legacy
      componentNamePattern: "my-org.example/*"
      versionConstraint: "<2.0.0"
```

{{< /tab >}}
{{< /tabs >}}

For the full version constraint syntax, see
[Version Constraints]({{< relref "docs/reference/resolver-configuration.md#version-constraints" >}}).

## Key Differences

|                      | Fallback (`ocm.config.ocm.software`)                              | Glob-based (`resolvers.config.ocm.software/v1alpha1`)                                  |
|----------------------|-------------------------------------------------------------------|----------------------------------------------------------------------------------------|
| **Matching**         | String prefix on component name                                   | Glob pattern (`*`, `?`, `[...]`) on component name, optional semver version constraint |
| **Resolution order** | Priority-based (highest first), then fallback through all matches | First match wins (list order)                                                          |
| **Get behaviour**    | Tries all matching repos until one succeeds                       | Returns the first matching repo deterministically                                      |
| **Add behaviour**    | Adds to the first matching repo by priority                       | Adds to the first matching repo by list order                                          |
| **Status**           | Deprecated                                                        | Active                                                                                 |

## Next Steps

- [How-To: Resolving Components Across Multiple Registries]
  ({{< relref "resolve-components-from-multiple-repositories.md" >}}) — Configure resolver
  entries for multi-registry setups

## Related Documentation

- [Resolver Configuration Reference]({{< relref "docs/reference/resolver-configuration.md" >}}) — Full configuration
  schema and pattern syntax
- [Resolvers]({{< relref "docs/concepts/resolvers.md" >}}) — High-level introduction to resolvers
