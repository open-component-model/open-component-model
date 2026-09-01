---
title: "Generating Software Bill of Materials"
description: "Overview of the SBOM-Generator extension and how it creates Software Bill of Materials documents."
weight: 6
toc: true
---

## What is the SBOM-Generator?

The SBOM-Generator is an ODG extension that automatically creates Software Bill of Materials (SBOM) documents for OCM resources. It uses [Syft](https://github.com/anchore/syft) to scan artefacts and stores the generated SBOMs in ODG blob storage for retrieval via the dashboard or API. It solves the problem of keeping SBOMs current across all component versions without manual tooling invocations.

## Why does it exist?

SBOMs are a compliance requirement for many software delivery processes, but generating and maintaining them manually across many components and versions is impractical. The SBOM-Generator integrates with ODG's backlog-driven scheduling so that SBOMs are regenerated automatically whenever a new component version is picked up, and the results are stored alongside other compliance metadata in ODG for unified reporting and download.

## How it works

### Component scanning process

When a component is picked up for scanning, the SBOM-Generator:

1. **Resolves the component descriptor** from configured OCM repositories
2. **Retrieves each Resource** from the component
3. **Scans using Syft CLI** via subprocess

The scanning approach varies by resource type:

**`ociRegistry` Resources**
: The image reference is passed directly to the Syft CLI.

**`localBlob/v1` Resources**
: The blob is downloaded to a temporary file before scanning.

**`s3` Resources**
: The tar archive is downloaded and extracted to a temporary directory before scanning.

### SBOM storage and metadata

Once the SBOM is produced:

1. **Serialisation**: The SBOM is serialised to JSON
2. **Hashing**: A SHA-256 hash is computed
3. **Upload**: The SBOM is uploaded to the ODG blob storage
4. **Metadata Recording**: The digest, file size, and output format are recorded as `ArtefactMetadata` of type `artefact_scan_info` for that resource

The dashboard queries this metadata to determine whether an SBOM is ready for download.

### Generation flow

<img src="/odg/sbom-generator-overview.svg" alt="SBOM Generation Overview">

### Supported output formats

The SBOM-Generator supports two standard SBOM formats:

- **CycloneDX** (default)
- **SPDX**

The format is configurable per ODG instance.

### Rescanning behaviour

Components are automatically rescanned based on the configured `interval` (default: 24 hours). This ensures that SBOMs remain up-to-date as component resources change.

## Key properties

| Property | Description |
| --- | --- |
| Scanning tool | Syft CLI (via subprocess) |
| Resource types supported | `ociRegistry`, `localBlob/v1`, `s3` |
| Output formats | CycloneDX (default), SPDX |
| Storage | ODG blob storage; metadata recorded as `artefact_scan_info` |
| Rescan interval | Configurable; default 24 hours |
| Format configuration | Per ODG instance |

## Relationship to other concepts

- [ODG System Architecture]({{< relref "docs/concepts/odg/odg-system-architecture/" >}}) — describes how the SBOM-Generator integrates as a backlog-driven extension
- [Correlating metadata to OCM]({{< relref "docs/concepts/odg/correlating-metadata-to-ocm/" >}}) — defines the `ArtefactMetadata` model used to record SBOM scan info
- [Reacting upon OCM events]({{< relref "docs/concepts/odg/reacting-upon-ocm-events/" >}}) — the artefact enumerator creates backlog items that trigger the SBOM-Generator

## When to use it

Refer to this document when:

- You want to understand **how ODG generates and stores SBOMs** for your components
- You are **configuring the output format** (CycloneDX vs. SPDX)
- You are **debugging why an SBOM is not available** in the dashboard
- You need to understand the **rescan interval** and when SBOMs are refreshed

## Next steps

- [Deploying the Open Delivery Gear locally]({{< relref "docs/how-to/odg/deploying-the-open-delivery-gear-locally/" >}})
- [Prepare your component for ODG]({{< relref "docs/how-to/odg/prepare-your-component-for-odg/" >}})
- [Setup from scratch (macOS)]({{< relref "docs/tutorials/odg/setup-from-scratch-macos/" >}})

## Related documentation

- [ODG System Architecture]({{< relref "docs/concepts/odg/odg-system-architecture/" >}})
- [Correlating metadata to OCM]({{< relref "docs/concepts/odg/correlating-metadata-to-ocm/" >}})
- [Reacting upon OCM events]({{< relref "docs/concepts/odg/reacting-upon-ocm-events/" >}})
- [Lifecycling GitHub Issues]({{< relref "docs/concepts/odg/lifecycling-github-issues/" >}})
- [Overwriting OCM component responsibles]({{< relref "docs/concepts/odg/overwriting-ocm-component-responsibles/" >}})
- [SLA Violation Profiler]({{< relref "docs/concepts/odg/sla-violation-profiler/" >}})
