---
title: "Credential Consumer Identities"
description: "Complete reference for OCM credential consumer identity types, their attributes, and credential properties."
icon: "🔑"
weight: 3
toc: true
---

This page is the technical reference for credential consumer identities — the key-value maps OCM uses to look up credentials for a given operation. For a high-level introduction, see [Credential System]({{< relref "docs/concepts/credential-system.md" >}}).

## Overview

Every time OCM needs credentials (accessing a registry, signing a component version), it constructs a **lookup identity** — a map of string attributes describing what it needs credentials for. The credential system then searches configured consumers for a matching entry.

A consumer entry in `.ocmconfig` looks like this:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: <identity-type>
          # ... type-specific attributes
        credentials:
          - type: Credentials/v1
            properties:
              # ... key-value credential properties
```

The consumer identity type is extensible — any string in `Name` or `Name/Version` format can be used.
Plugins and integrations can introduce additional types (e.g. `AWSSecretsManager`, `HashiCorpVault`, `MavenRepository`).
The following types are defined by the core OCM modules:

| Identity Type                                    | Used For |
|--------------------------------------------------|---|
| [`OCIRegistry`](#ociregistry)                    | Authenticating against OCI registries |
| [`HelmChartRepository`](#helmchartrepository)    | Authenticating against Helm chart repositories |
| [`RSA/v1alpha1`](#rsav1alpha1)                   | Providing signing and verification keys |

---

## OCIRegistry

Used when OCM accesses an OCI registry — pushing, pulling, or resolving component versions and resources.

### Identity Attributes

| Attribute | Required | Description |
|---|---|---|
| `type` | Yes | Must be `OCIRegistry` |
| `hostname` | Yes | Registry hostname (e.g. `ghcr.io`, `registry.example.com`) |
| `path` | No | Repository path. Supports glob patterns (`*` matches one path segment). If omitted, matches any path on the hostname. |
| `scheme` | No | URL scheme (`https`, `http`, `oci`). If omitted, matches any scheme. If set, must match exactly. |
| `port` | No | Port number as string. Default ports are applied when `scheme` is set: `https` and `oci` default to `443`, `http` defaults to `80`. |

### Credential Properties

| Property | Description |
|---|---|
| `username` | Registry username |
| `password` | Registry password or token |

### Matching Behavior

Matching runs three chained checks — all must pass:

1. **Path matcher** — compares `path` using `path.Match` (glob). `*` matches one segment, not across `/`. If the configured entry has no `path`, any request path is accepted.
2. **URL matcher** — compares `scheme`, `hostname`, and `port`. Applies default ports when a scheme is present (`https` → `443`, `http` → `80`).
3. **Equality matcher** — all remaining attributes (like `type`) must be exactly equal.

For detailed matching examples and edge cases, see [Tutorial: Understand Credential Resolution]({{< relref "docs/tutorials/credential-resolution.md" >}}).

### Examples

**Hostname only** — matches all paths on `ghcr.io`:

```yaml
- identity:
    type: OCIRegistry
    hostname: ghcr.io
  credentials:
    - type: Credentials/v1
      properties:
        username: my-user
        password: ghp_token
```

**Hostname + path glob** — matches any single-segment path under `my-org/`:

```yaml
- identity:
    type: OCIRegistry
    hostname: ghcr.io
    path: my-org/*
  credentials:
    - type: Credentials/v1
      properties:
        username: org-user
        password: ghp_org_token
```

**Hostname + scheme + port** — matches only HTTPS on a custom port:

```yaml
- identity:
    type: OCIRegistry
    hostname: registry.internal
    scheme: https
    port: "8443"
  credentials:
    - type: Credentials/v1
      properties:
        username: internal-user
        password: internal_pass
```

---

## HelmChartRepository

Used when OCM accesses a remote Helm chart repository — pulling or resolving Helm charts referenced as resources. The identity is derived from the Helm repository URL using the same URL-based attributes as `OCIRegistry`.

### Identity Attributes

| Attribute | Required | Description |
|---|---|---|
| `type` | Yes | Must be `HelmChartRepository` |
| `hostname` | Yes | Repository hostname (e.g. `charts.example.com`, `registry.example.com`) |
| `path` | No | Repository path (e.g. `stable`). If omitted, matches any path on the hostname. |
| `scheme` | No | URL scheme (`https`, `http`, `oci`). If omitted, matches any scheme. |
| `port` | No | Port number as string. If omitted, matches any port. |

### Credential Properties

| Property | Description |
|---|---|
| `username` | Repository username |
| `password` | Repository password or token |

### Examples

**HTTPS Helm repository:**

```yaml
- identity:
    type: HelmChartRepository
    hostname: charts.example.com
    path: stable
  credentials:
    - type: Credentials/v1
      properties:
        username: helm-user
        password: helm-token
```

**OCI-based Helm repository:**

```yaml
- identity:
    type: HelmChartRepository
    hostname: registry.example.com
    scheme: oci
  credentials:
    - type: Credentials/v1
      properties:
        username: registry-user
        password: registry-token
```

---

## RSA/v1alpha1

Used when OCM signs or verifies component versions with RSA keys.

### Identity Attributes

| Attribute | Required | Description |
|---|---|---|
| `type` | Yes | Must be `RSA/v1alpha1` |
| `algorithm` | Yes | Signing algorithm. Must be `RSASSA-PSS` (recommended) or `RSASSA-PKCS1-V1_5`. |
| `signature` | Yes | Logical signature name (e.g. `default`). Must match the `--signature` flag used with `ocm sign cv`. Defaults to `default` if not specified on the CLI. |

{{< callout context="caution" >}}
**All three attributes are required.** When OCM looks up signing credentials, it always constructs a lookup identity with `type`, `algorithm`, and `signature`. If your consumer entry omits `algorithm`, the credential system will not find a match — even though the signing algorithm defaults to `RSASSA-PSS` internally.

If you are unsure which algorithm to use, specify `algorithm: RSASSA-PSS`.
{{< /callout >}}

### Credential Properties

| Property | Used For | Description |
|---|---|---|
| `private_key_pem` | Signing | Inline PEM-encoded private key |
| `private_key_pem_file` | Signing | Path to PEM-encoded private key file |
| `public_key_pem` | Verification | Inline PEM-encoded public key |
| `public_key_pem_file` | Verification | Path to PEM-encoded public key file |

You can specify both `private_key_pem_file` and `public_key_pem_file` in the same entry to use it for both signing and verification.

### Matching Behavior

Unlike OCI identities, RSA signing identities use **strict equality matching** — every attribute in the lookup identity must be present in the configured consumer identity with the exact same value. There is no glob or subset matching.

### Examples

**Signing and verification with default settings:**

```yaml
- identity:
    type: RSA/v1alpha1
    algorithm: RSASSA-PSS
    signature: default
  credentials:
    - type: Credentials/v1
      properties:
        private_key_pem_file: /path/to/private-key.pem
        public_key_pem_file: /path/to/public-key.pem
```

**Multiple signature identities** (e.g. dev and prod):

```yaml
- identity:
    type: RSA/v1alpha1
    algorithm: RSASSA-PSS
    signature: dev
  credentials:
    - type: Credentials/v1
      properties:
        private_key_pem_file: /path/to/dev/private-key.pem
        public_key_pem_file: /path/to/dev/public-key.pem
- identity:
    type: RSA/v1alpha1
    algorithm: RSASSA-PSS
    signature: prod
  credentials:
    - type: Credentials/v1
      properties:
        private_key_pem_file: /path/to/prod/private-key.pem
        public_key_pem_file: /path/to/prod/public-key.pem
```

Sign with a specific identity:

```bash
ocm sign cv --signature dev <component-version>
ocm sign cv --signature prod <component-version>
```

**Using PKCS#1 v1.5 algorithm:**

```yaml
- identity:
    type: RSA/v1alpha1
    algorithm: RSASSA-PKCS1-V1_5
    signature: legacy
  credentials:
    - type: Credentials/v1
      properties:
        private_key_pem_file: /path/to/private-key.pem
```

---

## Complete Configuration Example

A single `.ocmconfig` combining registry credentials (with Docker fallback) and signing credentials:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      # OCI registry — hostname catch-all
      - identity:
          type: OCIRegistry
          hostname: ghcr.io
        credentials:
          - type: Credentials/v1
            properties:
              username: my-user
              password: ghp_token
      # RSA signing — default signature
      - identity:
          type: RSA/v1alpha1
          algorithm: RSASSA-PSS
          signature: default
        credentials:
          - type: Credentials/v1
            properties:
              private_key_pem_file: /path/to/private-key.pem
              public_key_pem_file: /path/to/public-key.pem
    # Docker config fallback for registries not matched above
    repositories:
      - repository:
          type: DockerConfig/v1
          dockerConfigFile: "~/.docker/config.json"
```

## Related Documentation

- [Concept: Credential System]({{< relref "docs/concepts/credential-system.md" >}}) — How the credential system works
- [Tutorial: Understand Credential Resolution]({{< relref "docs/tutorials/credential-resolution.md" >}}) — Step-by-step matching examples for OCI registries
- [How-To: Configure Credentials for Multiple Registries]({{< relref "docs/how-to/configure-multiple-credentials.md" >}}) — Task-oriented registry credential setup
- [How-To: Configure Credentials for Signing]({{< relref "docs/how-to/configure-signing-credentials.md" >}}) — Task-oriented signing credential setup
