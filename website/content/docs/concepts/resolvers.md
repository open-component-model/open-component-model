---
title: "Resolvers"
description: "Learn how OCM resolvers map component name patterns to repositories for recursive resolution."
icon: "🔍"
weight: 5
toc: true
---

## Why Resolvers?

In OCM, a component can **reference** other components. For example, an `app` component might reference a `backend` and
a `frontend` component. These referenced components don't have to live in the same repository as the app — and in
practice, they often don't. Teams publish components independently, to different registries or repository paths.

This creates a problem: when you ask the CLI to recursively resolve a component graph, it needs to know **where** to
find each referenced component. The repository you pass on the command line only tells the CLI where to find the
**root** component. For everything else, the CLI needs a mapping from component names to repositories.

That's what resolvers provide.

## What Are Resolvers?

A resolver maps a **component name pattern** (glob) to an
**[OCM repository](https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/01-model.md#component-repositories)**.
When the CLI encounters a component reference during recursive operations, it walks the list of configured resolvers,
finds the first pattern that matches the referenced component name, and queries the associated repository.

```mermaid
flowchart TD
    Start["CLI encounters a component reference"] --> Resolvers["Walk resolver list (in order)"]
    Resolvers --> Match{"Component name matched the next resolver's pattern?"}
    Match -- Yes --> Query["Query the resolver's configured repository"]
    Match -- No --> More{"More resolvers in the list?"}
    More -- Yes --> Resolvers
    More -- No --> Fail["Resolution fails: no matching resolver"]
    Query --> Found{"Component version found?"}
    Found -- Yes --> Done["Use this component version"]
    Found -- No --> Fail
```

## Configuration

Resolvers are configured in the OCM configuration file (by default `$HOME/.ocmconfig`). Each resolver entry maps a
component name pattern to a repository. Resolvers are evaluated in order — the first matching entry wins.

The repository field supports different repository types, including OCI registries and file-based CTF archives.
Component name patterns use common glob syntax (e.g., `*` for single-level, `**` for multi-level matching).

{{<callout context="tip">}}
For the full configuration schema, supported repository types, and pattern syntax details, see the
[Resolver Configuration Reference]({{< relref "docs/reference/resolver-configuration.md" >}}).
{{</callout>}}

## Recursive Resolution

When a component version has references to other component versions (via `componentReferences`), the CLI can follow
these references recursively using the `--recursive` flag. The CLI uses resolvers to locate each referenced component
in its respective repository — without them, recursive resolution across multiple repositories is not possible.

## OCM Transfer

Resolvers play an important role in transferring component versions across registries. When transferring a component
graph with `--recursive`, the CLI uses resolvers to locate each referenced component so it can copy the entire graph
to the target repository. Combined with `--copy-resources`, this enables full transfers of component graphs —
including all referenced resources — across registry boundaries or even air-gapped environments.

For more information about OCM transfer, see the [Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}}) concept.

## Next Steps

- [Tutorial: Working with Resolvers]({{< relref "docs/tutorials/configure-resolvers.md" >}}) — Hands-on walkthrough for setting up resolvers
- [How-To: Resolve Components Across Multiple Registries]({{< relref "docs/how-to/resolve-components-from-multiple-repositories.md" >}}) — Recipe for
  multi-registry resolution
- [How-To: Migrate from Deprecated Resolvers]({{< relref "docs/how-to/migrate-from-deprecated-resolvers.md" >}}) — Replace deprecated fallback
  resolvers with glob-based resolvers

## Related Documentation

- [Reference: Resolver Configuration]({{< relref "docs/reference/resolver-configuration.md" >}}) — Full schema, repository types, and pattern syntax
- [Component Identity]({{< relref "docs/concepts/component-identity.md" >}}) — Core concepts behind component versions, identities, and references
- [How-To: Transfer Components Across an Air Gap]({{< relref "docs/how-to/air-gap-transfer.md" >}}) — Use OCM Transfer to move components between air-gapped environments
- [Tutorial: Understand Credential Resolution]({{< relref "docs/tutorials/credential-resolution.md" >}}) — Configure credentials for OCI registries
