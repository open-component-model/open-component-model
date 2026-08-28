---
title: "Download SBOM Documents"
description: "Download Software Bill of Materials documents from the ODG Dashboard or API."
weight: 6
toc: true
---

## Goal

Download Software Bill of Materials (SBOM) documents for your component's OCM resources from ODG.

## You'll end up with

- SBOM documents downloaded for your product or a specific sub-component
- Understanding of which sub-components are ready for SBOM download and which are pending

**Estimated time:** ~5 minutes

## Prerequisites

- Your product must be added to the ODG Dashboard
- SBOM-Generator extension must be enabled in your ODG instance

## Steps

### Download SBOM for your product

The SBOM-Generator extension generates Software Bill of Materials (SBOM)
documents for the OCM resources of your components. It uses
[Syft](https://github.com/anchore/syft) to scan artefacts directly,
and stores the generated SBOMs in the ODG blob storage.

1. Open your product page in the ODG Dashboard.

2. Click the **DOWNLOAD SBOM** button.

   <img src="/odg/download-sbom-button.svg" alt="Download SBOM button">

   This opens the SBOM popover, where all sub-components are grouped into two
   sections: **Ready** and **Not ready**.

   <img src="/odg/download-sbom-popover.svg" alt="Download SBOM Popover">

   The popover also shows the configured output format, and displays the access
   type and artefact type for each sub-component.

   {{< callout type="tip" >}}
   The popover updates in real time. No manual refresh is needed.
   {{< /callout >}}

### Download SBOM for a sub-component

To download the SBOM for a specific sub-component:

1. Open the sub-component page in the ODG Dashboard
2. Click the **DOWNLOAD SBOM** button

### Manually trigger SBOM generation

If a component's SBOM has not been generated yet:

1. Open the **DOWNLOAD SBOM** popover for your component
2. Check the **Not ready** section for pending sub-components
3. Click the **Trigger SBOM generation** button when available

This schedules SBOM generation for all pending sub-components immediately. The
popover updates in real time, and completed SBOMs move from **Not ready** to
**Ready** as they finish.

## Troubleshooting

### Symptom: Sub-component stuck in "Not ready"

**Cause:** SBOM generation may have failed or not yet been triggered.

**Fix:** Click **Trigger SBOM generation** in the **Not ready** section. If generation continues to fail, see [Diagnose Failed SBOM Generation]({{< relref "docs/how-to/odg/diagnose-failed-sbom-generation/" >}}).

## Next steps

- [Diagnose failed SBOM generation]({{< relref "docs/how-to/odg/diagnose-failed-sbom-generation/" >}})

## Related documentation

- [Generating Software Bill of Materials]({{< relref "docs/concepts/odg/generating-software-bill-of-materials/" >}})
