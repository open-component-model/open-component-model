---
title: "Migrate from Fallback to Deterministic Repository Resolvers"
description: "Replace deprecated fallback resolvers with glob-based resolvers for deterministic and efficient component resolution."
weight: 10
toc: true
---

## Goal

Replace the deprecated `ocm.config.ocm.software` fallback resolver configuration with the new `resolvers.config.ocm.software/v1alpha1` glob-based
resolvers.

{{< callout context="caution" >}}
The fallback resolver (`ocm.config.ocm.software`) is deprecated. It uses priority-based ordering with prefix matching and probes multiple
repositories until one succeeds. The glob-based resolver (`resolvers.config.ocm.software/v1alpha1`) replaces it with first-match glob-based matching
against component names, which is simpler and more efficient.
{{< /callout >}}

## Why Migrate?

- The fallback resolver (`ocm.config.ocm.software`) is **deprecated** and will be removed in a future release.
- The fallback resolver probes multiple repositories on every lookup, which adds latency and makes it harder to reason about which repository is used.
- Glob-based resolvers use first-match semantics — the outcome is determined by list order alone, making configurations simpler to understand and debug.

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

The fallback resolver uses `prefix` to match component names by string prefix. The glob-based resolver uses `componentNamePattern`,
which supports [glob patterns]({{< relref "docs/reference/resolver-configuration.md#component-name-patterns" >}}).
In most cases, appending `/*` to the old prefix is the closest equivalent. Note that the old `prefix` also matched the bare prefix itself as an exact
component name (e.g. `prefix: my-org.example/services` matched both `my-org.example/services` and `my-org.example/services/foo`). If you have
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

Glob-based resolvers do not use priorities. Instead, resolvers are evaluated in the order they appear in the list, and the **first match wins**.
That's one of the key differences from the fallback resolver, which tries all matching resolvers in priority order until one succeeds.
If your legacy resolvers had equal `priority` values, keep their original list order to preserve the same resolution behaviour (the fallback resolver uses a stable sort, so equal-priority entries were tried in insertion order).

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

If you still see the warning `using deprecated fallback resolvers, consider switching to glob-based resolvers`, check that you removed all
`ocm.config.ocm.software` configuration blocks.
Both resolver types can coexist in the same config file during migration — the fallback resolvers will still work but will emit the deprecation
warning.

{{< /step >}}

{{< /steps >}}

## Key Differences

|                      | Fallback (`ocm.config.ocm.software`)                              | Glob-based (`resolvers.config.ocm.software/v1alpha1`) |
|----------------------|-------------------------------------------------------------------|-------------------------------------------------------|
| **Matching**         | String prefix on component name                                   | Glob pattern (`*`, `?`, `[...]`) on component name    |
| **Resolution order** | Priority-based (highest first), then fallback through all matches | First match wins (list order)                         |
| **Get behaviour**    | Tries all matching repos until one succeeds                       | Returns the first matching repo deterministically     |
| **Add behaviour**    | Adds to the first matching repo by priority                       | Adds to the first matching repo by list order         |
| **Status**           | Deprecated                                                        | Active                                                |

## When You Cannot Migrate Yet

The fallback resolver has a **probe-and-retry** behaviour that the glob-based resolver does not replicate.

Consider a registry migration where the same component has versions spread across multiple repositories:

| Version                             | Repository                     |
|-------------------------------------|--------------------------------|
| `my-org.example/my-component:1.0.0` | `old-registry.example/legacy`  |
| `my-org.example/my-component:1.5.0` | `old-registry.example/legacy`  |
| `my-org.example/my-component:2.0.0` | `new-registry.example/current` |

{{< tabs >}}
{{< tab "Fallback (works)" >}}

The fallback resolver matches repositories by **prefix** and tries them in **priority order**. If the requested version is not found in the
highest-priority repository, it falls back to the next one until it finds a match or exhausts the list.

In this example, both resolvers share the prefix `my-org.example`. A request for `my-component:2.0.0` finds it in the new registry (priority 10). A
request for `my-component:1.0.0` misses in the new registry, falls back to the old one (priority 1), and succeeds.

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
{{< tab "Glob-based (breaks)" >}}

The glob-based resolver matches the component name against patterns in list order and returns the **first match** — it does not probe the repository
or retry with the next resolver if the version is missing.

In this example, `componentNamePattern: "my-org.example/*"` points to the new registry only. Requesting `my-component:2.0.0` succeeds, but requesting
`my-component:1.0.0` fails because the old registry is not configured and there is no fallback.

```yaml
  - type: resolvers.config.ocm.software/v1alpha1
    resolvers:
      # Only new-registry is queried — old versions are unreachable
      - repository:
          type: OCIRepository/v1
          baseUrl: new-registry.example
          subPath: current
        componentNamePattern: "my-org.example/*"
```

{{< /tab >}}
{{< /tabs >}}

The same applies to **listing component versions**: the fallback resolver accumulates versions from all matching repositories, while the glob-based
resolver only queries the first match.

If either case applies, consolidate all versions of the affected components into a single repository before migrating your resolver config. Version-based matching is being tracked as a future feature in [ocm-project#941](https://github.com/open-component-model/ocm-project/issues/941).

## Next Steps

- [How-To: Resolving Components Across Multiple Registries]({{< relref "resolve-components-from-multiple-repositories.md" >}}) — Configure resolver
  entries for multi-registry setups

## Related Documentation

- [Resolver Configuration Reference]({{< relref "docs/reference/resolver-configuration.md" >}}) — Full configuration schema and pattern syntax
- [Resolvers]({{< relref "docs/concepts/resolvers.md" >}}) — High-level introduction to resolvers
