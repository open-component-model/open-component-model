---
title: "Migrate Legacy Credentials"
description: "Update your legacy OCM credential configuration to use modern field names and optional typed credentials."
icon: "🔑"
weight: 11
toc: true
---

## Goal

Update a legacy OCM `.ocmconfig` file to use modern field names and optional typed credentials. Most fields work unchanged in the new OCM; this guide covers the one renamed field (`pathprefix` → `path`) and the optional migration to typed credentials.

{{< callout context="caution" >}}
`HashiCorpVault/v1`, `GardenerConfig/v1`, and `NPMConfig/v1` are not yet available in the new OCM. If you rely on these,
stay on legacy OCM for now.
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

The field for matching repository paths was renamed from `pathprefix` to `path`. Because `pathprefix` matched any path
starting with the given prefix, you need to append a glob pattern (`/*`) to preserve the same matching behavior:

```yaml
    consumers:
      - identity:
          type: OCIRegistry
          hostname: ghcr.io
          path: open-component-model/*  # was: pathprefix: open-component-model
```

{{< callout context="note" >}}
`path` does **not** do prefix matching — `path: open-component-model` would only match the exact path
`open-component-model`, not `open-component-model/my-repo`. Use `open-component-model/*` to match any single segment
after the prefix, or `open-component-model/*/*` for two levels.
{{< /callout >}}

{{< /step >}}

{{< step >}}
**Change `identity` to `identities` (optional)**

The new OCM still accepts the singular `identity` field, so this step is optional. However, switching to `identities` (a list) lets you share one credential across multiple registries. See [Multi-Identity Credentials]({{< relref "/docs/tutorials/credential-resolution.md" >}}) for details.

```yaml
    consumers:
      - identities: # was: identity (now a list)
          - type: OCIRegistry
            hostname: ghcr.io
            path: open-component-model/*
```

{{< /step >}}

{{< step >}}
**Migrate to typed credentials**

Replace `Credentials/v1` with the typed equivalent for your credential type. Typed credentials use flat top-level fields
instead of a nested `properties:` map and are validated at parse time.

For OCI registries, replace:

```yaml
        credentials:
          - type: Credentials/v1
            properties:
              username: my-user
              password: my-token
```

with:

```yaml
        credentials:
          - type: OCICredentials/v1
            username: my-user
            password: my-token
```

`OCICredentials/v1` also supports `accessToken` and `refreshToken` for token-based auth. For all built-in typed credential types and their fields, see [Reference: Credential Types]({{< relref "/docs/reference/credential-types.md" >}}).

{{< callout context="note" >}}
`Credentials/v1` continues to work unchanged — it is an alias for `DirectCredentials/v1`. You only need this step if you
want field validation and cleaner configuration.
{{< /callout >}}

{{< /step >}}

{{< step >}}
**Verify**

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
          - type: OCICredentials/v1
            username: my-user
            password: my-token
    repositories:
      - repository:
          type: DockerConfig/v1
          dockerConfigFile: "~/.docker/config.json"
```

Run any OCM command that requires authentication:

```bash
ocm get cv ghcr.io/my-org/my-component
```

If you get `unknown credential repository type`, you may be using a repository type not yet supported in the new OCM (
`HashiCorpVault/v1`, `NPMConfig/v1`, `GardenerConfig/v1`). Remove the unsupported entry or stay on legacy OCM until
support is added.

If you get `401 Unauthorized`, check that you renamed `pathprefix` → `path` (with a glob pattern) in all consumer
entries.

{{< /step >}}

{{< /steps >}}

## Next Steps

- [How-To: Configure Credentials for Multiple Registries]({{< relref "docs/how-to/configure-multiple-credentials.md" >}}) - Set up credentials for multiple registries
- [Tutorial: Credential Resolution]({{< relref "/docs/tutorials/credential-resolution.md" >}}) - Learn how OCM resolves
  credentials step-by-step

## Related Documentation

- [Concept: Credential System]({{< relref "/docs/concepts/credential-system.md" >}}) - Learn how the credential system
  automatically finds the right credentials for each operation
- [Reference: Credential Types]({{< relref "/docs/reference/credential-types.md" >}}) - All built-in typed credential
  types and their fields
