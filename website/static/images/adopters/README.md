# Adopter logos

Logos in this directory power the *Trusted by* marquee on the OCM landing page.
They are shown for editorial purposes only. Everything below this section of the
README is maintainer internal; only the next section is public.

## Trademark attribution

The adopter logos are trademarks or registered trademarks of their respective
owners. Inclusion of a logo on this site does not imply endorsement of the
Open Component Model by the trademark owner, and it does not grant any right to
reuse those marks. If you want to use any of these marks, contact the respective
owner.

| Mark             | Owner                                |
|------------------|--------------------------------------|
| SAP              | SAP SE                               |
| BWI              | BWI GmbH                             |
| SAP NS2          | SAP National Security Services, Inc. |
| Gardener         | NeoNephos Foundation                 |
| Konfidence       | SAP SE / Konfidence project          |
| Kyma             | SAP SE / Kyma project                |
| OpenControlPlane | NeoNephos Foundation                 |
| Platform Mesh    | NeoNephos Foundation                 |
| T-Systems        | T-Systems International GmbH         |
| STACKIT          | STACKIT GmbH & Co. KG                |
| IronCore         | NeoNephos Foundation                 |
| EverBright       | EverBright GmbH                      |

"Open Component Model" and the OCM logo are assets of the Open Component Model
project, governed by the NeoNephos Foundation's trademark policy:
<https://linuxfoundation.eu/policies/trademark-policy>.

If you own a mark shown here and want it corrected or removed, open an issue at
<https://github.com/open-component-model/open-component-model/issues>.

---

## Maintainer notes

The entries below record source URL, fetch date, licence rationale, trademark
owner, and any customisation applied to each logo.

**No explicit permission emails were sent.** Usage is based on editorial/factual
use to indicate OCM adoption. If any owner objects, files will be removed
immediately.

### How to add a new adopter

The marquee renders every logo at a uniform **44 px height**; the width follows
each asset's intrinsic aspect ratio (capped at **280 px**). Spacing between logos
is handled by the track's `gap`, so adjacent logos keep identical optical distance
regardless of how wide each one is.

For this to "just work" when you drop in a new logo, follow these steps:

1. **File naming.** Drop a light-theme SVG/PNG into this folder, named
   `<org>-light.svg` (or `<org>-color.svg` if one asset serves both themes).
   Add `<org>-dark.svg` only when the light variant is unreadable on dark
   backgrounds.

2. **Normalise the SVG root.** The `<svg>` element MUST declare both
   `width` and `height` attributes matching the `viewBox`, with the `viewBox`
   cropped to the visible artwork (no extra padding). Reason: when the root
   lacks `width`/`height`, browsers fall back to a 300×150 intrinsic size and
   the marquee layout becomes unpredictable.

   Required pattern:

   ```svg
   <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 W H" width="W" height="H">
     …
   </svg>
   ```

   Where `W` and `H` are the same numbers as the `viewBox`. Example (T-Systems):

   ```svg
   <svg viewBox="0 0 1530.99 314.14" width="1530.99" height="314.14" …>
   ```

   For a PNG, the natural pixel size serves as intrinsic size — no edit needed.
   Confirm with an `<img>` test: open the file in a browser tab and check that
   "Image size" matches the expected aspect ratio.

3. **Crop the viewBox to the artwork.** If the SVG ships with branding padding
   inside the viewBox (common for exports from Figma or Illustrator), trim it.
   Tools: `svgo` will not remove padding, so use Inkscape
   (*Edit → Resize Page to Selection*), Adobe Illustrator, or a free online
   SVG viewer/editor. The goal: the `viewBox` x/y/w/h describe exactly the
   visible pixels of the logo.

4. **Register the adopter.** In `website/content/_index.md`, add to
   `adopters:` the keys `name`, `logo_light`, `logo_dark`, `url`. Do NOT add
   `width` or any other size override; the CSS doesn't read them.

5. **Update this README.** Append a section `### <Organisation>` at the bottom
   with the provenance table: File, Source, Fetched (date), Licence, Trademark
   owner, and Notes on any customisation you applied.

6. **Verify.** Start the dev server (`npm run dev`) and check
   <http://localhost:1313/main/>:
   - Every logo renders at exactly **44 px tall** (DevTools inspect).
   - Spacing between adjacent logos is consistent (the track provides it via `gap`).
   - The marquee does not overflow horizontally on a ~390 px wide viewport.
   - Light and dark theme both render.

If any of these fail, fix the SVG (step 2 and 3), not the CSS.

---

### SAP

- **File:** `sap-color.svg`
- **Source:** Wikimedia Commons (`File:SAP_2011_logo.svg`)
- **Fetched:** 2026-10-26
- **Licence:** Public domain (PD-textlogo / simple shapes)
- **Trademark owner:** SAP SE

### BWI (BWI GmbH)

- **File:** `bwi-color.svg`
- **Source:** Wikimedia Commons
- **Fetched:** 2026-10-26
- **Licence:** Public domain (PD-textlogo)
- **Trademark owner:** BWI GmbH

### SAP NS2 (SAP National Security Services)

- **File:** `sap-ns2.png` (raster, 900×500)
- **Source:** getlogovector.com
- **Fetched:** 2026-10-26
- **Licence:** Editorial use only (no explicit licence found)
- **Trademark owner:** SAP National Security Services, Inc.

### Gardener

- **File:** `gardener-color.svg`
- **Source:** GitHub `neonephos/artwork` repo
- **Fetched:** 2026-10-26
- **Licence:** NeoNephos trademark usage guidelines
- **Trademark owner:** NeoNephos Foundation

### Konfidence

- **File:** `konfidence-dark.svg`
- **Source:** konfidence.cloud
- **Fetched:** 2026-10-26
- **Licence:** Editorial use (no explicit licence)
- **Trademark owner:** SAP SE / Konfidence project

### Kyma

- **File:** `kyma-light.svg` / `kyma-dark.svg` (icon + label lockup built from `kyma-color.svg`)
- **Source:** kyma-project.io
- **Fetched:** 2026-10-26
- **Licence:** Editorial use (no explicit licence)
- **Trademark owner:** SAP SE / Kyma project

### Open Control Plane (OCP)

- **File:** `ocp-light.svg` / `ocp-dark.svg` (icon + label lockup built from `ocp-color.svg`)
- **Source:** open-control-plane.io
- **Fetched:** 2026-10-26
- **Licence:** NeoNephos trademark usage guidelines
- **Trademark owner:** NeoNephos Foundation
- **Note:** `ocp-dark.svg` remaps the original dark blues to lighter tones for dark-mode visibility; stroke colour switched from `#221f20` to `#f5f5f7`.

### Platform Mesh

- **File:** `platform-mesh-color.svg` / `platform-mesh-dark.svg`
- **Source:** GitHub `neonephos/artwork` repo
- **Fetched:** 2026-10-26
- **Licence:** NeoNephos trademark usage guidelines
- **Trademark owner:** NeoNephos Foundation

### T-Systems

- **File:** `t-systems-light.svg` (magenta `#e20074` on light) and `t-systems-dark.svg` (white on dark)
- **Source:** Wikimedia Commons (`File:T-Systems Logo 2024.svg`)
- **Fetched:** 2026-10-27
- **Licence:** Public domain (PD-textlogo)
- **Trademark owner:** T-Systems International GmbH

### STACKIT

- **File:** `stackit-light.svg` (petrol `#004e5a` on light) and `stackit-dark.svg` (white on dark)
- **Source:** GitHub `stackitcloud/stackit-sdk-go` repo (`.github/images/`)
- **Fetched:** 2026-10-26
- **Licence:** Editorial use (official brand asset in project repo)
- **Trademark owner:** STACKIT GmbH & Co. KG
- **Note:** Dark variant derived from the official light SVG by replacing the fill colour with white.

### IronCore

- **File:** `ironcore-light.svg` / `ironcore-dark.svg` (icon + label lockup built from `ironcore-color.svg`)
- **Source:** GitHub `ironcore-dev/ironcore` repo (`docs/assets/`)
- **Fetched:** 2026-10-26
- **Licence:** NeoNephos trademark usage guidelines
- **Trademark owner:** NeoNephos Foundation

### EverBright

- **File:** `everbright-light.svg` (dark text on light) and `everbright-dark.svg` (white on dark)
- **Source:** everbright-it.de header asset
- **Fetched:** 2026-10-26
- **Licence:** Editorial use (no explicit licence; file is the site's own header logo)
- **Trademark owner:** EverBright GmbH (per Impressum on everbright-it.de)
