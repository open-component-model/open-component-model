---
title: "Resolver Configuration"
description: "Complete reference for OCM resolver configuration: schema, repository types, and component name patterns."
icon: "🔍"
weight: 5
toc: true
---

This page is the technical reference for OCM resolver configuration. For a high-level introduction, see
[OCM Resolvers]({{< relref "docs/concepts/resolvers.md" >}}).

## Configuration File

Resolvers are configured in the OCM configuration file. By default, the CLI searches for configuration in
`$HOME/.ocmconfig`. You can also specify a configuration file explicitly with the `--config` flag.

{{<callout context="tip">}}
For more information about the OCM configuration file,
see [.ocmconfig documentation](https://github.com/open-component-model/ocm/blob/main/docs/reference/ocm_configfile.md).
{{</callout>}}

The resolver configuration uses the type `resolvers.config.ocm.software/v1alpha1` inside a generic OCM configuration
type:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: resolvers.config.ocm.software/v1alpha1
    resolvers:
      - repository:
          type: OCIRepository/v1
          baseUrl: ghcr.io
          subPath: my-org/components
        componentNamePattern: "example.com/services/*"
        versionConstraint: ">=1.0.0 <2.0.0"
```

## Config Schema

The resolver configuration is defined by the `resolvers.config.ocm.software/v1alpha1` type in
the [OCM specification](https://github.com/open-component-model/open-component-model/tree/main/bindings/go/configuration/resolvers/v1alpha1/spec).

| Field       | Type   | Required | Description                                       |
|-------------|--------|----------|---------------------------------------------------|
| `type`      | string | Yes      | Must be `resolvers.config.ocm.software/v1alpha1`. |
| `resolvers` | array  | No       | List of resolver entries.                         |

### Resolver Entry Schema

| Field                  | Type   | Required | Description                                                                                       |
|------------------------|--------|----------|---------------------------------------------------------------------------------------------------|
| `repository`           | object | Yes      | An OCM repository specification (must include a `type` field).                                    |
| `componentNamePattern` | string | No       | Glob pattern for matching component names. If omitted, the resolver matches all components.       |
| `versionConstraint`    | string | No       | Semver constraint for matching component versions. If omitted, the resolver matches all versions. |

## Repository Types

The `repository` field accepts any OCM repository specification. The most common types are:

{{< tabs >}}
{{< tab "OCI Registry" >}}

```yaml
repository:
  type: OCIRepository/v1
  baseUrl: ghcr.io
  subPath: my-org/components
```

| Field     | Required | Description                                                          |
|-----------|----------|----------------------------------------------------------------------|
| `type`    | Yes      | Repository type. Must be `OCIRepository/v1`.                         |
| `baseUrl` | Yes      | Registry host and optional port (e.g., `ghcr.io`, `localhost:5000`). |
| `subPath` | No       | Repository prefix path within the registry.                          |

{{< /tab >}}
{{< tab "CTF (File-based)" >}}

```yaml
repository:
  type: CommonTransportFormat/v1
  filePath: /path/to/ctf-archive
```

| Field        | Required | Description                                                                                      |
|--------------|----------|--------------------------------------------------------------------------------------------------|
| `type`       | Yes      | Repository type. Accepted values include `CommonTransportFormat/v1` and the short form `CTF/v1`. |
| `filePath`   | Yes      | Path to the CTF archive file or directory.                                                       |
| `accessMode` | No       | Access mode: `readonly`, `readwrite`, or `create`.                                               |

{{< /tab >}}
{{< /tabs >}}

## Component Name Patterns

Each resolver entry can include a `componentNamePattern` field that uses **glob patterns** to filter for certain
component names.
Only components matching the pattern will be routed to that repository.

**Supported glob patterns:**

| Pattern                  | Matches                                                 |
|--------------------------|---------------------------------------------------------|
| `example.com/services/*` | Any component directly under `example.com/services/`    |
| `example.com/core/**`    | Any component under `example.com/core/` or its subpaths |
| `*`                      | All components (wildcard catch-all)                     |

**Pattern syntax:**

- `*` — Matches any sequence of characters within a path segment
- `**` — Matches any sequence of characters in subpath segments
- `?` — Matches any single character
- `[abc]` — Matches any character in the set (a, b, or c)
- `[a-z]` — Matches any character in the range

{{<callout context="note">}}
For more information on the supported glob syntax, see the [glob package documentation](https://github.com/gobwas/glob).
{{</callout>}}

## Version Constraints

Each resolver entry can include a `versionConstraint` field that uses **semver constraints** to filter for certain
component versions.
Only components with versions satisfying the constraint will be routed to that repository.

**Supported constraint syntax:**

| Constraint       | Matches                                                       |
|------------------|---------------------------------------------------------------|
| `>=1.0.0 <2.0.0` | Versions from 1.0.0 (inclusive) up to but not including 2.0.0 |
| `^1.2.0`         | Compatible with 1.2.0 (>=1.2.0 <2.0.0)                        |
| `~1.2.0`         | Approximately 1.2.0 (>=1.2.0 <1.3.0)                          |
| `1.2.3`          | Exactly version 1.2.3                                         |

Multiple space-separated constraints are combined with AND logic (e.g., `>=1.0.0 <2.0.0` means both must be satisfied).

{{<callout context="note">}}
For more information on the supported semver constraint syntax, see
the [Masterminds/semver documentation](https://github.com/Masterminds/semver#checking-version-constraints).
{{</callout>}}

## Resolver Evaluation Order

Resolvers are evaluated **in the order they are defined**.
The first matching resolver wins. Place more specific patterns before broader ones.

## Related Documentation

- [OCM Resolvers]({{< relref "docs/concepts/resolvers.md" >}}) — High-level introduction to resolvers
- [Working with Resolvers Tutorial]({{< relref "docs/tutorials/configure-resolvers.md" >}}) — Hands-on walkthrough for
  setting up resolvers
- [How to Resolve Components Across Multiple Registries]
- ({{< relref "docs/how-to/resolve-components-from-multiple-repositories.md" >}}) — Recipe for multi-registry resolution
- [Migrate from Deprecated Resolvers]({{< relref "docs/how-to/migrate-from-deprecated-resolvers.md" >}}) — Replace
  deprecated fallback
  resolvers with glob-based resolvers
