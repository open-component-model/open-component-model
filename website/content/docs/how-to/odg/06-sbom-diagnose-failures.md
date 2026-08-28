---
title: "Diagnose Failed SBOM Generation"
description: "Troubleshoot SBOM generation failures in ODG."
weight: 7
toc: true
---

This guide is for operators who need to troubleshoot SBOM generation failures.

## Prerequisites

- Access to the ODG Dashboard with operator permissions
- SBOM-Generator extension enabled

## View SBOM Generation Logs

1. Open the **SBOM-Generator** section in the ODG Dashboard sidebar

2. Review the logs which show:
   - Status of each run
   - Errors and warnings
   - Timestamps

This information makes it straightforward to identify and diagnose issues with
SBOM generation.

## Common Issues

### Missing AWS Credentials for S3 Artefacts

If you see errors related to S3 access, ensure that:
- The appropriate AWS secret is configured in the `mappings` section
- The `aws_secret_name` matches the secret name in your ODG instance

### Unsupported Artefact Types

The SBOM-Generator currently supports:
- `ociRegistry` resources (container images)
- `localBlob/v1` resources
- `s3` resources (tar archives)

Other resource types will not be scanned.

### Timeout Issues

If generation times out:
- Check the `interval` configuration
- Verify that the artefact is accessible
- Review network connectivity to the artefact source
