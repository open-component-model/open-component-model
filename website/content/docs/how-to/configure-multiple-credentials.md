---
title: "Configure Credentials for Multiple Registries"
description: "Set up OCM credentials to authenticate against multiple OCI registries with explicit entries and Docker config fallback."
icon: "🔑"
weight: 3
toc: true
---

## Goal

Configure OCM to authenticate against multiple OCI registries — pinning explicit credentials for specific registries while using Docker config as a catch-all fallback.

## You'll end up with

- An OCM config file that resolves credentials automatically for every `ocm` command
- Explicit control over specific registry paths with Docker config covering the rest

## Prerequisites

- [OCM CLI]({{< relref "/docs/getting-started/ocm-cli-installation.md" >}}) installed
- Docker CLI installed and `docker login` run for any registries you want as fallback (creates `~/.docker/config.json`)

## Steps

{{< steps >}}

{{< step >}}
**Identity by hostname only**

Create `$HOME/.ocmconfig` with an identity that matches **any path** on a hostname:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      # Hostname-only identity
      - identities:
          - type: OCIRegistry
            hostname: ghcr.io  # No path = matches all paths
        credentials:
          - type: Credentials/v1
            properties:
              username: my-user
              password: ghp_your_token_here
```

**Identity:** `hostname: ghcr.io` (no `path`)  
**Matches:** `ghcr.io/any-org/any-repo`, `ghcr.io/foo/bar`, etc. — all paths on this hostname.
{{< /step >}}

{{< step >}}
**Identity by hostname + exact path**

Add an identity that matches a **specific path** on a hostname:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      # ... other consumers
      # Hostname + exact path identity
      - identities:
          - type: OCIRegistry
            hostname: ghcr.io
            path: my-org/production  # Exact path match
        credentials:
          - type: Credentials/v1
            properties:
              username: prod-user
              password: ghp_production_token
```

**Identity:** `hostname: ghcr.io` + `path: my-org/production`  
**Matches:** Only `ghcr.io/my-org/production` (exact match)  
**Differs from step 1:** The first consumer now matches only one specific path. The second consumer (hostname-only) catches everything else on `ghcr.io`.
{{< /step >}}

{{< step >}}
**Identity by hostname + path pattern**

Add an identity that matches **multiple paths** using a glob pattern:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      # ... other consumers
      # Hostname + path glob identity
      - identities:
          - type: OCIRegistry
            hostname: ghcr.io
            path: my-org/*  # Glob — matches one segment after my-org/
        credentials:
          - type: Credentials/v1
            properties:
              username: org-user
              password: ghp_org_token
```

**Identity:** `hostname: ghcr.io` + `path: my-org/*`  
**Matches:** `ghcr.io/my-org/staging`, `ghcr.io/my-org/dev`, etc. — any single segment after `my-org/`  
**Differs from step 2:** Uses a glob pattern (`*`) instead of an exact path. More specific identities (like `path: my-org/production`) are checked first.
{{< /step >}}

{{< step >}}
**Identity via Docker config (fallback)**

Add a `repositories` section to use Docker config for **any registry** not matched by consumers:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
    # ... other consumers
    # Docker config fallback
    repositories:
      - repository:
          type: DockerConfig/v1
          dockerConfigFile: "~/.docker/config.json"  # Fallback for unmatched registries
```

**Identity:** Derived from the `auths` section in Docker config file (`~/.docker/config.json`)  
**Matches:** Any registry with credentials in Docker config that wasn't matched by a consumer  
**Differs from steps 1-3:** This is a **repository** (not a consumer). OCM checks all consumers first, then falls back to repositories. Use this for registries you authenticate with `docker login`.
{{< /step >}}

{{< /steps >}}

## Complete Example Configuration

Here's a complete configuration combining all identity types:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      # Path glob: matches ghcr.io/my-org/staging, ghcr.io/my-org/dev, etc.
      - identities:
          - type: OCIRegistry
            hostname: ghcr.io
            path: my-org/*
        credentials:
          - type: Credentials/v1
            properties:
              username: org-user
              password: ghp_org_token
      # Exact path: matches only ghcr.io/my-org/production (takes precedence over glob)
      - identities:
          - type: OCIRegistry
            hostname: ghcr.io
            path: my-org/production
        credentials:
          - type: Credentials/v1
            properties:
              username: prod-user
              password: ghp_production_token
      # Hostname only: matches all other paths on ghcr.io
      - identities:
          - type: OCIRegistry
            hostname: ghcr.io
        credentials:
          - type: Credentials/v1
            properties:
              username: my-user
              password: ghp_your_token_here
    # Docker config: fallback for any registry not matched above
    repositories:
      - repository:
          type: DockerConfig/v1
          dockerConfigFile: "~/.docker/config.json"
```

**Resolution order** (see [Credential Resolution Tutorial]({{< relref "/docs/tutorials/credential-resolution.md" >}}) for details):

1. `ghcr.io/my-org/production` → uses `prod-user` (exact path match)
2. `ghcr.io/my-org/staging` → uses `org-user` (glob `my-org/*` match)
3. `ghcr.io/other-org/repo` → uses `my-user` (hostname-only match)
4. `docker.io/library/nginx` → uses Docker config (no consumer matched)

## Troubleshooting

{{< callout context="tip" >}}
Use `ocm --loglevel debug` to enable debug logging. This shows which consumers are checked and how credentials are resolved.
{{< /callout >}}

### Symptom: `401 Unauthorized` for a registry with explicit credentials

**Cause:** The consumer identity doesn't match the request. Common issues: mismatched `path` or `hostname`.

**Fix:** Check that `hostname` and `path` match the registry URL exactly. Remember that `path: my-org` does **not** match `my-org/production`.

### Symptom: `401 Unauthorized` for a registry that should use Docker fallback

**Cause:** Docker config doesn't have credentials for that registry.

**Fix:** Run `docker login <registry-hostname>`, then retry the OCM command.

## Related Documentation

- [How-To: Migrate Legacy Credentials]({{< relref "legacy-credential-compatibility.md" >}}) - Migrate an existing legacy OCM `.ocmconfig` file so it works with the new OCM
- [Tutorial: Credential Resolution]({{< relref "/docs/tutorials/credential-resolution.md" >}}) — learn how OCM resolves credentials by experimenting step-by-step
- [Concept: Credential System]({{< relref "/docs/concepts/credential-system.md" >}}) - Learn how the credential system automatically finds the right credentials for each operation
