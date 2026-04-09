---
title: "Migrate Legacy Credentials"
description: "Update your legacy OCM credential configuration to work with the new OCM."
icon: "🔑"
weight: 9
toc: true
---

## Goal

Migrate an existing legacy OCM `.ocmconfig` file so it works with the new OCM.

{{< callout context="caution" >}}
`HashiCorpVault/v1`, `GardenerConfig/v1`, and `NPMConfig/v1` are not yet available in the new OCM. If you rely on these, stay on legacy OCM for now.
{{< /callout >}}

## Prerequisites

- [OCM CLI]({{< relref "/docs/getting-started/ocm-cli-installation.md" >}}) installed
- An existing `.ocmconfig` file from legacy OCM

## Steps

Suppose you have the following legacy config in `$HOME/.ocmconfig`:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: OCIRegistry
          hostname: ghcr.io
          pathprefix: open-component-model
        credentials:
          - type: Credentials/v1
            properties:
              username: my-user
              password: my-token
    repositories:
      - repository:
          type: DockerConfig/v1
          dockerConfigFile: "~/.docker/config.json"
```

The following steps walk you through each change needed to make this config work with the new OCM.

{{< steps >}}

{{< step >}}
**Change `pathprefix` to `path` with a glob pattern**

The field for matching repository paths was renamed from `pathprefix` to `path`. Because `pathprefix` matched any path starting with the given prefix, you need to append a glob pattern (`/*`) to preserve the same matching behavior:

```yaml
    consumers:
      - identity:
          type: OCIRegistry
          hostname: ghcr.io
          path: open-component-model/*  # was: pathprefix: open-component-model
```

{{< callout context="note" >}}
`path` does **not** do prefix matching — `path: open-component-model` would only match the exact path `open-component-model`, not `open-component-model/my-repo`. Use `open-component-model/*` to match any single segment after the prefix, or `open-component-model/*/*` for two levels.
{{< /callout >}}

{{< /step >}}

{{< step >}}
**Change `identity` to `identities` (optional)**

The new OCM still accepts the singular `identity` field, so this step is optional. However, switching to `identities` (a list) lets you share one credential across multiple registries. See [Multi-Identity Credentials]({{< relref "/docs/tutorials/credential-resolution.md" >}}) for details.

```yaml
    consumers:
      - identities:  # was: identity (now a list)
          - type: OCIRegistry
            hostname: ghcr.io
            path: open-component-model/*
```

{{< /step >}}

{{< step >}}
**Keep everything else as-is**

The following parts of your legacy config work unchanged in the new OCM:

- `OCIRegistry` consumer identity type (unchanged)
- `Credentials/v1` type and `properties` field
- `DockerConfig/v1` repository entries
- `dockerConfigFile` and `dockerConfig` fields
- Config file locations (`$HOME/.ocmconfig`, `$OCM_CONFIG`)

Your migrated config now looks like this:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identities:
          - type: OCIRegistry
            hostname: ghcr.io
            path: open-component-model/*
        credentials:
          - type: Credentials/v1
            properties:
              username: my-user
              password: my-token
    repositories:
      - repository:
          type: DockerConfig/v1
          dockerConfigFile: "~/.docker/config.json"
```

{{< /step >}}

{{< step >}}
**Verify**

Run any OCM command that requires authentication:

```bash
ocm get cv ghcr.io/my-org/my-component
```

If you get `unknown credential repository type`, you may be using a repository type not yet supported in the new OCM (`HashiCorpVault/v1`, `NPMConfig/v1`, `GardenerConfig/v1`). Remove the unsupported entry or stay on legacy OCM until support is added.

If you get `401 Unauthorized`, check that you renamed `pathprefix` → `path` (with a glob pattern) in all consumer entries.

{{< /step >}}

{{< /steps >}}

## Next Steps

- [How-To: Configure Credentials for Multiple Registries]({{< relref "docs/how-to/configure-multiple-credentials.md" >}}) - Set up credentials for multiple registries
- [Tutorial: Credential Resolution]({{< relref "/docs/tutorials/credential-resolution.md" >}}) - Learn how OCM resolves credentials step-by-step

## Related Documentation

- [Concept: Credential System]({{< relref "/docs/concepts/credential-system.md" >}}) - Learn how the credential system automatically finds the right credentials for each operation
