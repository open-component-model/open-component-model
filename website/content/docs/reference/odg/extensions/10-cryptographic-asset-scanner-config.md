---
title: "Cryptographic Asset Scanner"
description: "Configuration reference for the Cryptographic Asset Scanner extension."
weight: 11
toc: true
---

The Cryptographic Asset Scanner extension identifies cryptographic assets within components and creates findings for non-compliant cryptographic usage against configured standards.

## Configuration Example

```yaml
crypto:
  interval: 86400                    # 24 hours
  on_unsupported: warning
  mappings:
    - prefix: 'acme.org/product-a'
      included_asset_types:
        - algorithm
        - certificate
        - library
      libraries:
        - ref:
            path: odg/crypto_defaults.yaml
      standards:
        - name: FIPS
          version: 140-3
          ref:
            path: odg/crypto_defaults.yaml
      aws_secret_name: aws-account-prod
    - prefix: ''                     # catch-all
      included_asset_types: null     # all types
      libraries:
        - ref:
            path: odg/crypto_defaults.yaml
      standards:
        - name: FIPS
          version: 140-3
          ref:
            path: odg/crypto_defaults.yaml
```

## Top-Level Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `interval` | int (seconds) | `86400` | Maximum time before a component is re-scanned. |
| `on_unsupported` | string | `warning` | Behaviour when artefact kind/type/access is unsupported. Options: `fail`, `ignore`, `warning`. |
| `mappings` | list | `[]` | Per-prefix component mappings. See mapping fields below. |

## Mapping Fields

Each entry in the `mappings` list supports the following fields:

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `prefix` | string | yes | Component name prefix. Use `''` (empty string) to match all components. |
| `standards` | list | yes | Cryptographic standards to validate against. See standard fields below. |
| `libraries` | list | yes | References to files containing known cryptographic library names. |
| `included_asset_types` | list or null | no | Filter which cryptographic asset types to analyse. `null` = all types. Options: `algorithm`, `certificate`, `library`, `protocol`, `related-crypto-material`. |
| `aws_secret_name` | string | no | Name of the AWS secret to use for S3 artefacts. |

## Standard Fields

Each entry in the `standards` list defines a cryptographic compliance standard:

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `name` | string | yes | Standard name (e.g., `FIPS`). |
| `version` | string | yes | Standard version (e.g., `140-3`). |
| `ref.path` | string | yes | Relative path to file containing standard definitions. |

## Library Reference Fields

Each entry in the `libraries` list can be a string (library name) or a reference object:

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `ref.path` | string | yes | Relative path to file in odg-core repository, external GitHub repo, or OCM resource. |

## Configuration Details

### `interval`

The maximum time (in seconds) before a component's cryptographic assets are
re-analysed. Default is 86400 seconds (24 hours).

This interval ensures:
- Cryptographic compliance is regularly re-evaluated
- New standards or library definitions are applied to existing components
- Changes in artefacts are detected and analysed

### `on_unsupported`

Defines the behaviour when an artefact kind, type, or access method is not supported:

- **`fail`**: Raise an exception and stop processing
- **`ignore`**: Silently skip the unsupported artefact
- **`warning`** (default): Skip the artefact and log a warning message

### `mappings`

Allows per-component-prefix configuration for cryptographic analysis. This is useful when:
- Different components must comply with different standards
- Some components require analysis of specific asset types only
- Components are stored in different AWS S3 accounts

#### Prefix Matching

The `prefix` field uses simple string prefix matching:
- `prefix: 'acme.org'` matches `acme.org/product` and `acme.org/another-product`
- `prefix: ''` (empty string) matches all components (use as a catch-all)

Multiple mappings are evaluated in order, and the first matching prefix is used.

### `standards`

Defines cryptographic compliance standards to validate against. Each standard requires:
- A name and version identifier
- A reference to a YAML file containing the standard's compliance rules

The referenced file must contain a `standards` property with the rules for that standard.

**Example standard reference:**

```yaml
standards:
  - name: FIPS
    version: 140-3
    ref:
      path: odg/crypto_defaults.yaml
```

### `libraries`

Lists known cryptographic library names used for validation. Libraries can be:
- Inline strings (direct library names)
- References to YAML files containing a `libraries` property

**Example with reference:**

```yaml
libraries:
  - ref:
      path: odg/crypto_defaults.yaml  # File in odg-core repo
```

**Example with inline names:**

```yaml
libraries:
  - OpenSSL
  - BoringSSL
  - wolfSSL
```

You can also reference files in external GitHub repositories or OCM resources.

### `included_asset_types`

Filters which cryptographic asset types to analyse. Set to `null` to analyse all types,
or provide a list to restrict analysis to specific categories:

- **`algorithm`**: Cryptographic algorithms (AES, RSA, SHA, etc.)
- **`certificate`**: X.509 certificates and certificate chains
- **`library`**: Cryptographic libraries (OpenSSL, BoringSSL, etc.)
- **`protocol`**: Cryptographic protocols (TLS, SSH, etc.)
- **`related-crypto-material`**: Keys, seeds, nonces, and other crypto material

**Examples:**

```yaml
# Analyse all asset types
included_asset_types: null

# Only analyse algorithms and libraries
included_asset_types:
  - algorithm
  - library

# Only analyse certificates
included_asset_types:
  - certificate
```

### `aws_secret_name`

When scanning artefacts stored in AWS S3, specify which AWS secret to use for authentication.
This is required when multiple AWS secrets are configured in ODG.

```yaml
aws_secret_name: aws-production-account
```
