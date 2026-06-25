---
title: "Credential Types"
description: "Reference for all built-in OCM credential types and their configuration fields."
icon: "🔑"
weight: 4
toc: true
---

This page is the technical reference for OCM's built-in credential types — the values you place in the `credentials:` field of a consumer entry. For a high-level introduction, see [Credential System]({{< relref "docs/concepts/credential-system.md" >}}).

## Overview

Every consumer entry in `.ocmconfig` has a `credentials:` list. Each entry in that list has a `type` that determines
which fields are expected:

```yaml
consumers:
  - identity:
      type: OCIRegistry
      hostname: registry.example.com
    credentials:
      - type: <credential-type>
        # ... type-specific fields
```

OCM ships with the following built-in credential types:

| Credential Type                                            | Used With                                | Purpose                                                         |
|------------------------------------------------------------|------------------------------------------|-----------------------------------------------------------------|
| [`OCICredentials/v1`](#ocicredentialsv1)                   | `OCIRegistry` consumers                  | OCI registry username/password and token auth                   |
| [`HelmHTTPCredentials/v1`](#helmhttpcredentialsv1)         | `HelmChartRepository` consumers (HTTP/S) | Helm HTTP repository auth and TLS client certs                  |
| [`RSACredentials/v1`](#rsacredentialsv1)                   | `RSA/v1alpha1` consumers                 | RSA signing and verification key material                       |
| [`GPGCredentials/v1alpha1`](#gpgcredentialsv1alpha1)       | `GPG/v1alpha1` consumers                 | GPG signing and verification key material                       |
| [`OIDCIdentityToken/v1alpha1`](#oidcidentitytokenv1alpha1) | `SigstoreSigner/v1alpha1` consumers      | OIDC token for Sigstore keyless signing via Fulcio              |
| [`TrustedRoot/v1alpha1`](#trustedrootv1alpha1)             | `SigstoreVerifier/v1alpha1` consumers    | Sigstore trust material for private infrastructure verification |
| [`DirectCredentials/v1`](#directcredentialsv1)             | Any consumer                             | Legacy untyped key-value fallback (also `Credentials/v1`)       |

All typed credential types use flat top-level fields. `DirectCredentials/v1` uses a nested `properties:` map — it is the
universal fallback and all existing `.ocmconfig` files using `Credentials/v1` continue to work unchanged.

---

## Discovering Credential Types

The `ocm describe types` command exposes the full set of credential types registered in your OCM installation, including
any types introduced by installed plugins.

**List all available credential types:**

```bash
ocm describe types credentials
```

**Inspect the fields of a specific credential type:**

```bash
ocm describe types credentials OCICredentials/v1
```

**Export the raw JSON Schema for a type** (useful for tooling and validation):

```bash
ocm describe types credentials OCICredentials/v1 -o jsonschema
```

---

## OCICredentials/v1

{{< schema-renderer url="/schemas/bindings/go/credentials/oci/v1/OCICredentials.schema.json" >}}

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

[`OCIRegistry`]({{< relref "credential-consumer-identities.md#ociregistry" >}}) consumer identities. For OCI-backed Helm
repositories, also use `OCICredentials/v1` (not `HelmHTTPCredentials/v1`).

---

## HelmHTTPCredentials/v1

{{< schema-renderer url="/schemas/bindings/go/credentials/helm/v1/HelmHTTPCredentials.schema.json" >}}

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

[`HelmChartRepository`]({{< relref "credential-consumer-identities.md#helmchartrepository" >}}) consumer identities that
use HTTP/S transport. For OCI-based Helm repositories, use `OCICredentials/v1` instead.

---

## RSACredentials/v1

{{< schema-renderer url="/schemas/bindings/go/credentials/rsa/v1/RSACredentials.schema.json" >}}

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

## GPGCredentials/v1alpha1

{{< schema-renderer url="/schemas/bindings/go/credentials/gpg/v1alpha1/GPGCredentials.schema.json" >}}

### Example

```yaml
consumers:
  - identity:
      type: GPG/v1alpha1
      algorithm: application/vnd.ocm.signature.gpg
      signature: default
    credentials:
      - type: GPGCredentials/v1alpha1
        privateKeyPGPFile: /path/to/private.asc
        publicKeyPGPFile: /path/to/public.asc
        passphrase: my-passphrase
```

### Used With

`GPG/v1alpha1` consumer identities (signing and verification with OpenPGP keys).

---

## OIDCIdentityToken/v1alpha1

{{< schema-renderer url="/schemas/bindings/go/credentials/sigstore/oidcidentitytoken/v1alpha1/OIDCIdentityToken.schema.json" >}}

### Example

```yaml
consumers:
  - identity:
      type: SigstoreSigner/v1alpha1
      signature: default
    credentials:
      - type: OIDCIdentityToken/v1alpha1
        tokenFile: /path/to/oidc-token
```

### Used With

`SigstoreSigner/v1alpha1` consumer identities (Sigstore keyless signing path only; not used during verification).

---

## TrustedRoot/v1alpha1

{{< schema-renderer url="/schemas/bindings/go/credentials/sigstore/trustedroot/v1alpha1/TrustedRoot.schema.json" >}}

### Example

```yaml
consumers:
  - identity:
      type: SigstoreVerifier/v1alpha1
      signature: default
    credentials:
      - type: TrustedRoot/v1alpha1
        trustedRootJSONFile: /path/to/trusted-root.json
```

### Used With

`SigstoreVerifier/v1alpha1` consumer identities (Sigstore verification path only; not used during signing).

---

## DirectCredentials/v1

{{< schema-renderer url="/schemas/bindings/go/credentials/direct/v1/DirectCredentials.schema.json" >}}

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
          privateKeyPEMFile: /path/to/private-key.pem
          publicKeyPEMFile: /path/to/public-key.pem
```

{{< callout context="note" >}}
`DirectCredentials/v1` property keys use the same **camelCase** names as the corresponding typed credential fields (
e.g., `privateKeyPEMFile`, not `private_key_pem_file`). The RSA binding additionally accepts the old `snake_case` keys (
`private_key_pem_file`, `public_key_pem_file`) as a deprecated backward-compatibility fallback — all other bindings
require camelCase.
{{< /callout >}}

### When to use

Use `Credentials/v1` / `DirectCredentials/v1` when:

- Migrating from an existing configuration and you want no changes
- Working with consumer identity types that do not yet have a corresponding typed credential type

Use the typed alternatives (`OCICredentials/v1`, `HelmHTTPCredentials/v1`, `RSACredentials/v1`) for new configurations —
they provide field validation at configuration parse time and clearer field names.

---

## Plugin-Declared Types

Plugins can introduce additional credential types beyond the built-ins listed here. A plugin declares its custom types
in the `customCredentialTypes` field of its capability spec. Those types are registered at plugin discovery time and
available in `.ocmconfig` alongside the built-ins.

External plugin credential types use a reverse-domain prefix by convention (e.g.,
`com.hashicorp.vault.VaultCredentials/v1`). This prevents name collisions between independently developed plugins.

For details on how plugins declare and register credential types, see
[Plugin System]({{< relref "docs/concepts/plugin-system.md" >}}).

---

## Related Documentation

- [Reference: Credential Consumer Identities]({{< relref "credential-consumer-identities.md" >}}) — identity types and
  their attributes
- [Concept: Credential System]({{< relref "docs/concepts/credential-system.md" >}}) — how credential resolution works
  end-to-end
- [Tutorial: Understand Credential Resolution]({{< relref "docs/tutorials/credential-resolution.md" >}}) — step-by-step
  matching examples
