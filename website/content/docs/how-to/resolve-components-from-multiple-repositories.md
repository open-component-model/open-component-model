---
title: "Resolving Components across Multiple Registries"
description: "Configure resolvers to recursively resolve component references distributed across multiple OCI registries."
weight: 11
toc: true
---

## Goal

Configure an `.ocmconfig` file with resolver entries so the OCM CLI can recursively resolve component references stored
in different OCI registries.

## Prerequisites

- [OCM CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) installed
- [Components]({{< relref "docs/getting-started/create-component-version.md" >}}) already pushed to separate OCI registries
- An OCM configuration file (see [.ocmconfig documentation](https://github.com/open-component-model/ocm/blob/main/docs/reference/ocm_configfile.md))

## Steps

{{< steps >}}

{{< step >}}
**Create an `.ocmconfig` with resolver entries**

Add a resolver entry for each registry where referenced components are stored. Replace the placeholder values with your
own registry, paths, and component names.

Assume you have a root component `<root-component>` that references `<component-a>` and `<component-b>`, each stored in
a different repository:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    repositories:
      - repository:
          type: DockerConfig/v1
          dockerConfigFile: "~/.docker/config.json"
  - type: resolvers.config.ocm.software/v1alpha1
    resolvers:
      - repository:
          type: OCIRepository/v1
          baseUrl: <registry>                 # e.g. ghcr.io
          subPath: <subpath-a>                # e.g. my-org/team-a
        componentNamePattern: "<component-a>" # e.g. my-org.example/component-a
      - repository:
          type: OCIRepository/v1
          baseUrl: <registry>                 # e.g. ghcr.io
          subPath: <subpath-b>                # e.g. my-org/team-b
        componentNamePattern: "<component-b>" # e.g. my-org.example/component-b
```

{{< /step >}}

{{< step >}}
**Resolve the root component recursively**

Point the CLI at the root component and pass your config file:

```bash
ocm add cv --repository <registry>/<root-subpath> \
  --constructor <constructor>.yaml \
  --config .ocmconfig
```

or

```bash
ocm get cv <registry>/<root-subpath>//<root-component>:<version> \
  --recursive=-1 --config .ocmconfig
```

`add cv` uploads a component version to the registry using the resolver config to locate referenced components.
`get cv --recursive` walks the full component graph and lists all transitively referenced components.

<details>
<summary>Example</summary>

```bash
ocm add cv --repository ghcr.io/my-org/components \
  --constructor root-constructor.yaml \
  --config .ocmconfig
```

```bash
ocm get cv ghcr.io/my-org/components//my-org.example/root-component:1.0.0 \
  --recursive=-1 --config .ocmconfig
```

</details>

{{< /step >}}

{{< step >}}
**Verify the output**

The output should list the root component and all transitively referenced components. If a referenced component is
missing, check that there is a matching resolver entry for it.

<details>
<summary>Example output</summary>

```text
COMPONENT                          │ VERSION │ PROVIDER
───────────────────────────────────┼─────────┼──────────
my-org.example/root-component      │ 1.0.0   │ my-org
my-org.example/component-a         │ 1.0.0   │
my-org.example/component-b         │ 1.0.0   │
```

</details>

{{< /step >}}

{{< /steps >}}

{{< callout context="tip" >}}
If you are migrating from the deprecated `ocm.config.ocm.software` fallback resolvers, see [Migrate from Deprecated Resolvers]({{< relref "migrate-from-deprecated-resolvers.md" >}}) for a step-by-step guide.
{{< /callout >}}

## Tips

- **If multiple components share a registry path**, use a glob pattern (e.g. `example.com/services/*`) instead of
  listing each component individually. See
  [Component Name Patterns]({{< relref "docs/reference/resolver-configuration.md#component-name-patterns" >}}) for the full syntax.
- **If you need to transfer components to another registry**, use `ocm transfer cv --recursive --copy-resources` with
  the same config file. See
  [OCM Transfer]({{< relref "docs/concepts/resolvers.md#ocm-transfer" >}}) for details.
- **Resolvers are evaluated in order** — place more specific patterns before broader ones so the right repository is
  matched first.

## Next Steps

- [Tutorial: Understand Credential Resolution]({{< relref "docs/tutorials/credential-resolution.md" >}}) — Configure registry credentials
- [Tutorial: Working with Resolvers]({{< relref "docs/tutorials/configure-resolvers.md" >}}) — Hands-on tutorial for
  building and pushing components with resolvers

## Related Documentation

- [How-To: Migrate from Deprecated Resolvers]({{< relref "docs/how-to/migrate-from-deprecated-resolvers.md" >}}) — Replace deprecated fallback
  resolvers with glob-based resolvers
- [Concept: Resolvers]({{< relref "docs/concepts/resolvers.md" >}}) — High-level introduction to resolvers
- [Reference: Resolver Configuration]({{< relref "docs/reference/resolver-configuration.md" >}}) — Full configuration schema, repository types, and pattern syntax
