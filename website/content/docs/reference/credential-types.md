---
title: "Credential Types"
description: "Reference for all built-in OCM credential types and their configuration fields."
icon: "🔑"
weight: 4
toc: true
---

This page is the technical reference for OCM's built-in credential types — the values you place in the `credentials:` field of a consumer entry. For a high-level introduction, see [Credential System]({{< relref "docs/concepts/credential-system.md" >}}).

## Overview

Every consumer entry in `.ocmconfig` has a `credentials:` list. Each entry in that list has a `type` that determines which fields are expected:

```yaml
consumers:
  - identity:
      type: OCIRegistry
      hostname: registry.example.com
    credentials:
      - type: <credential-type>
        # ... type-specific fields
```

OCM ships with four built-in credential types:

| Credential Type | Used With | Purpose |
|---|---|---|
| [`OCICredentials/v1`](#ocicredentialsv1) | `OCIRegistry` consumers | OCI registry username/password and token auth |
| [`HelmHTTPCredentials/v1`](#helmhttpcredentialsv1) | `HelmChartRepository` consumers (HTTP/S) | Helm HTTP repository auth and TLS client certs |
| [`RSACredentials/v1`](#rsacredentialsv1) | `RSA/v1alpha1` consumers | RSA signing and verification key material |
| [`DirectCredentials/v1`](#directcredentialsv1) | Any consumer | Legacy untyped key-value fallback (also `Credentials/v1`) |

Typed credential types (`OCICredentials/v1`, `HelmHTTPCredentials/v1`, `RSACredentials/v1`) use flat top-level fields. `DirectCredentials/v1` uses a nested `properties:` map — it is the universal fallback and all existing `.ocmconfig` files using `Credentials/v1` continue to work unchanged.

---

## OCICredentials/v1

Typed credentials for OCI registry authentication. Supports username/password basic auth and token-based flows.

### Fields

| Field | Description |
|---|---|
| `username` | Username for basic authentication |
| `password` | Password for basic authentication |
| `accessToken` | Bearer token sent directly to the registry (Docker token flow) |
| `refreshToken` | OAuth2 refresh token exchanged for an `accessToken` before each request |

All fields are optional; set only the fields applicable to your authentication method. Token fields (`accessToken`, `refreshToken`) take precedence over `username`/`password` when both are present.

### Example

```yaml
consumers:
  - identity:
      type: OCIRegistry
      hostname: registry.example.com
    credentials:
      - type: OCICredentials/v1
        username: my-user
        password: my-password
```

Token-based authentication:

```yaml
consumers:
  - identity:
      type: OCIRegistry
      hostname: registry.example.com
    credentials:
      - type: OCICredentials/v1
        refreshToken: my-oauth2-refresh-token
```

### Used With

[`OCIRegistry`]({{< relref "credential-consumer-identities.md#ociregistry" >}}) consumer identities. For OCI-backed Helm repositories, also use `OCICredentials/v1` (not `HelmHTTPCredentials/v1`).

---

## HelmHTTPCredentials/v1

Typed credentials for HTTP/S-based Helm chart repositories. Supports username/password and mutual TLS.

### Fields

| Field | Description |
|---|---|
| `username` | Username for basic authentication |
| `password` | Password for basic authentication |
| `certFile` | Path to a TLS client certificate file (PEM) |
| `keyFile` | Path to a TLS client private key file (PEM) |
| `keyring` | Path to a GPG keyring file for chart verification |

All fields are optional.

### Example

Username/password:

```yaml
consumers:
  - identity:
      type: HelmChartRepository
      hostname: charts.example.com
      scheme: https
    credentials:
      - type: HelmHTTPCredentials/v1
        username: helm-user
        password: helm-password
```

Mutual TLS:

```yaml
consumers:
  - identity:
      type: HelmChartRepository
      hostname: charts.internal
      scheme: https
    credentials:
      - type: HelmHTTPCredentials/v1
        certFile: /path/to/client.crt
        keyFile: /path/to/client.key
```

### Used With

[`HelmChartRepository`]({{< relref "credential-consumer-identities.md#helmchartrepository" >}}) consumer identities that use HTTP/S transport. For OCI-based Helm repositories, use `OCICredentials/v1` instead.

---

## RSACredentials/v1

Typed credentials carrying RSA key material for signing and verification. Each field has two forms: inline PEM content or a path to a PEM file. The inline form takes precedence when both are set.

### Fields

| Field | Description |
|---|---|
| `privateKeyPEM` | Inline PEM-encoded RSA private key (PKCS#1 or PKCS#8). Required for signing. |
| `privateKeyPEMFile` | Path to a PEM file containing an RSA private key. Used when `privateKeyPEM` is not set. |
| `publicKeyPEM` | Inline PEM-encoded RSA public key or X.509 certificate chain. Required for verification. |
| `publicKeyPEMFile` | Path to a PEM file containing an RSA public key. Used when `publicKeyPEM` is not set. |

For signing, provide `privateKeyPEM` or `privateKeyPEMFile`. For verification, provide `publicKeyPEM` or `publicKeyPEMFile`. Both can be combined in the same entry to support signing and verification from one config block.

### Example

File-based (recommended):

```yaml
consumers:
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PSS
      signature: default
    credentials:
      - type: RSACredentials/v1
        privateKeyPEMFile: /path/to/private-key.pem
        publicKeyPEMFile: /path/to/public-key.pem
```

Inline keys:

```yaml
consumers:
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PSS
      signature: default
    credentials:
      - type: RSACredentials/v1
        privateKeyPEM: |
          -----BEGIN RSA PRIVATE KEY-----
          MIIEpAIBAAKCAQEA...
          -----END RSA PRIVATE KEY-----
        publicKeyPEM: |
          -----BEGIN PUBLIC KEY-----
          MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A...
          -----END PUBLIC KEY-----
```

### Used With

[`RSA/v1alpha1`]({{< relref "credential-consumer-identities.md#rsav1alpha1" >}}) consumer identities.

---

## DirectCredentials/v1

The universal legacy fallback, also accepted as `Credentials/v1`. All existing `.ocmconfig` files continue to work unchanged — `Credentials/v1` is an alias for `DirectCredentials/v1`.

Unlike the typed credential types above, `DirectCredentials/v1` stores credentials as an untyped `properties:` map. The property key names are consumer-defined string constants (e.g., `username`, `password`, `private_key_pem_file`).

### Fields

| Field | Description |
|---|---|
| `properties` | Map of string key-value pairs. Key names depend on the consumer. |

### Example

OCI registry with `Credentials/v1`:

```yaml
consumers:
  - identity:
      type: OCIRegistry
      hostname: registry.example.com
    credentials:
      - type: Credentials/v1
        properties:
          username: my-user
          password: my-password
```

RSA signing with `Credentials/v1`:

```yaml
consumers:
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

{{< callout context="note" >}}
Note the difference in key naming: `DirectCredentials/v1` uses `snake_case` string keys (e.g., `private_key_pem_file`), while typed credential types like `RSACredentials/v1` use `camelCase` Go struct fields (e.g., `privateKeyPEMFile`).
{{< /callout >}}

### When to use

Use `Credentials/v1` / `DirectCredentials/v1` when:
- Migrating from an existing configuration and you want no changes
- Working with consumer identity types that do not yet have a corresponding typed credential type

Use the typed alternatives (`OCICredentials/v1`, `HelmHTTPCredentials/v1`, `RSACredentials/v1`) for new configurations — they provide field validation at configuration parse time and clearer field names.

---

## Plugin-Declared Types

Plugins can introduce additional credential types beyond the built-ins listed here. A plugin declares its custom types in the `customCredentialTypes` field of its capability spec. Those types are registered at plugin discovery time and available in `.ocmconfig` alongside the built-ins.

External plugin credential types use a reverse-domain prefix by convention (e.g., `com.hashicorp.vault.VaultCredentials/v1`). This prevents name collisions between independently developed plugins.

For details on how plugins declare and register credential types, see [Plugin System]({{< relref "docs/concepts/plugin-system.md" >}}).

---

## Related Documentation

- [Reference: Credential Consumer Identities]({{< relref "credential-consumer-identities.md" >}}) — identity types and their attributes
- [Concept: Credential System]({{< relref "docs/concepts/credential-system.md" >}}) — how credential resolution works end-to-end
- [Tutorial: Understand Credential Resolution]({{< relref "docs/tutorials/credential-resolution.md" >}}) — step-by-step matching examples
