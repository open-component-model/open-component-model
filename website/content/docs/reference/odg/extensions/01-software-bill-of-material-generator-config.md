---
title: "Software Bill of Material Generator"
description: "Configuration reference for the SBOM Generator extension."
weight: 2
toc: true
---

The Software Bill of Material Generator extension generates SBOM documents for OCM artefacts. It is configured under the `sbom_generator` key.

## Configuration Example

```yaml
sbom_generator:
  interval: 86400                         # 24 hours
  on_unsupported: warning
  generation_mode: syft                   # or 'bdba'
  output_format: cyclonedx                # or 'spdx', 'bdio'
  create_new_scan_if_missing: false       # BDBA mode only
  processing_mode: force_upload           # or 'rescan' (BDBA mode)
  mappings:
    - prefix: 'acme.org/product-a'
      group_id: 1234                      # Required for BDBA mode
      aws_secret_name: aws-account-prod
    - prefix: ''                          # catch-all
      group_id: 9999
      aws_secret_name: aws-account-default
```

## Top-Level Options

| Option | Type | Default | Description |
| --- | --- | --- | --- |
| `interval` | int (seconds) | `86400` | Maximum time before an artefact is re-scanned. |
| `on_unsupported` | string | `warning` | Behaviour when artefact kind/type/access is unsupported. Options: `fail`, `ignore`, `warning`. |
| `generation_mode` | string | `syft` | SBOM generation tool: `syft`, `bdba`. |
| `output_format` | string | `cyclonedx` | Output format: `cyclonedx`, `spdx`, `bdio`. |
| `create_new_scan_if_missing` | bool | `false` | BDBA mode: create new scan if none exists. |
| `processing_mode` | string | `force_upload` | BDBA mode: `rescan` (reuse binary) or `force_upload` (always re-upload). |
| `mappings` | list | `[]` | Per-prefix component mappings. See mapping fields below. |

## Mapping Fields

Each entry in the `mappings` list supports the following fields:

| Option | Type | Required | Description |
| --- | --- | --- | --- |
| `prefix` | string | yes | Component name prefix. Use `''` to match all components. |
| `group_id` | int | conditional | BDBA group ID (required when `generation_mode: bdba`). |
| `aws_secret_name` | string | no | Name of the AWS secret used to access S3 artefacts. |

## Configuration Details

### `interval`

The maximum time (in seconds) before a component's artefacts are rescanned.
Default is 86400 seconds (24 hours).

### `on_unsupported`

Defines the behaviour when an artefact kind, type, or access method is not supported:

- **`fail`**: Raise an exception and stop processing
- **`ignore`**: Silently skip the unsupported artefact
- **`warning`** (default): Skip the artefact and log a warning message

### `generation_mode`

Specifies which tool to use for SBOM generation:

- **`syft`** (default): Uses Syft for fast, local SBOM generation
- **`bdba`**: Retrieves SBOMs from BDBA scan results

When using `bdba` mode, additional fields (`group_id`, `create_new_scan_if_missing`, `processing_mode`) become relevant.

### `output_format`

Specifies the SBOM format to generate:

- **`cyclonedx`** (default): Generates CycloneDX format SBOMs
- **`spdx`**: Generates SPDX format SBOMs
- **`bdio`**: Generates Black Duck I/O format (BDBA mode only)

### `create_new_scan_if_missing`

Only applicable when `generation_mode: bdba`. When `true`, creates a new BDBA scan
if no existing scan is found. When `false` (default), skips SBOM generation if no
scan exists.

### `processing_mode`

Only applicable when `generation_mode: bdba`. Determines how existing BDBA scans are handled:

- **`rescan`**: Reuse the previously uploaded binary and retrieve updated results
- **`force_upload`** (default): Always re-upload the binary and retrieve updated results

### `mappings`

Allows per-component-prefix configuration. This is particularly useful when:

- Different components require different AWS credentials for S3 access
- You need to handle components from different sources differently

#### Prefix Matching

The `prefix` field uses simple string prefix matching:

- `prefix: 'acme.org'` matches `acme.org/product` and `acme.org/another-product`
- `prefix: ''` (empty string) matches all components (use as a catch-all)

Multiple mappings are evaluated in order, and the first matching prefix is used.

#### AWS Secret Configuration

When scanning S3 resources, the SBOM-Generator needs AWS credentials. The
`aws_secret_name` field specifies which AWS secret to use from your ODG
secrets configuration.

**Example with multiple prefixes:**

```yaml
sbom_generator:
  output_format: cyclonedx
  interval: 86400
  mappings:
    - prefix: 'acme.org/product-a'
      aws_secret_name: aws-account-prod
    - prefix: 'acme.org'
      aws_secret_name: aws-account-dev
    - prefix: ''
      aws_secret_name: aws-account-default
```
