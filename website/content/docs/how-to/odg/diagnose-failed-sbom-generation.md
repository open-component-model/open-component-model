---
title: "Diagnose Failed SBOM Generation"
description: "Troubleshoot SBOM generation failures in ODG."
weight: 7
toc: true
---

## Goal

Identify and resolve SBOM generation failures in your ODG instance.

## You'll end up with

- A diagnosis of why SBOM generation failed
- A resolution applied for the specific failure type

**Estimated time:** ~10 minutes

## Prerequisites

- Access to the ODG Dashboard with operator permissions
- SBOM-Generator extension enabled

## Steps

### View SBOM generation logs

1. Open the **SBOM-Generator** section in the ODG Dashboard sidebar

2. Review the logs which show:
   - Status of each run
   - Errors and warnings
   - Timestamps

This information makes it straightforward to identify and diagnose issues with SBOM generation.

## Troubleshooting

### Symptom: Errors related to S3 access

**Cause:** Missing or misconfigured AWS credentials for S3 artefacts.

**Fix:** Ensure that the appropriate AWS secret is configured in the `mappings` section and that `aws_secret_name` matches the secret name in your ODG instance.

### Symptom: Certain artefact types are not scanned

**Cause:** The artefact type is not supported by SBOM-Generator. The extension currently supports:

- `ociRegistry` resources (container images)
- `localBlob/v1` resources
- `s3` resources (tar archives)

**Fix:** Other resource types will not be scanned. No action required if the resource type is intentionally unsupported.

### Symptom: SBOM generation times out

**Cause:** Slow artefact access, network connectivity issues, or an interval configured too short.

**Fix:** Check the `interval` configuration, verify that the artefact is accessible, and review network connectivity to the artefact source.

## Next steps

- [Download SBOM documents]({{< relref "docs/how-to/odg/download-sbom-documents/" >}})

## Related documentation

- [Generating Software Bill of Materials]({{< relref "docs/concepts/odg/generating-software-bill-of-materials/" >}})
