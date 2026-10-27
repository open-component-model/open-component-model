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

Logos are used as downloaded. A few of the initial 12 assets below carry a
"Notes" line describing a one-off adjustment (e.g. a recoloured dark variant)
because the source only shipped one theme. This is the exception, not the
process: new logos should come in ready for both themes or as a single
universal file, without edits.

### How to add a new adopter

Logos must have their artwork touching (or within 2–3 px of) all four edges of
the SVG viewBox. A viewBox with significant empty space around the artwork
pushes the visual gaps between logos out of rhythm. This is the only edit you
ever need to make to an SVG; everything else stays as downloaded.

1. **Get the logo.** Download the official SVG (preferred) or PNG from the
   adopter's press/brand page or their GitHub org's artwork repository.

2. **Normalise the viewBox (SVG only).** Open the file in a text editor and
   find the root `<svg viewBox="x y w h">`. Load the file in Inkscape (or a
   browser + DevTools canvas trick) to find the tight bounding box of the
   artwork. Set `x` and `y` to the artwork's top-left corner, `w` and `h` to
   the artwork's width and height. Keep at most 2–3 SVG units of breathing room
   on each side. Do NOT change any path or shape data. For PNGs skip this step.

3. **Dark mode check.** Many logos work in both light and dark theme as a
   single file (any logo with strong colour and no near-black or near-white text
   does). If the logo only works on one theme, get the official variant for the
   other theme from the same source. Name light-only assets `<org>-light.svg`,
   dark-only assets `<org>-dark.svg`, universally usable ones `<org>-color.svg`.

4. **Register in `website/content/_index.md`.** Example:

   ```yaml
   adopters:
     - name: "Example Corp"
       logo_light: "images/adopters/example-color.svg"
       logo_dark:  "images/adopters/example-color.svg"
       url: "https://example.com"
   ```

   One file for both themes: point `logo_light` and `logo_dark` at the same
   file. Two variants: point each at its own file. No size or spacing keys
   exist; the CSS handles sizing (uniform height, width by aspect ratio) and
   spacing (fixed gap) automatically.

5. **Record provenance.** Append a section `### <Organisation>` at the bottom
   of this file with: File, Source URL, Fetched (date), Licence, Trademark
   owner. If the file needed a viewBox normalisation, note it there.

6. **Verify.** Run `npm run dev` and open <http://localhost:1313/main/>. Toggle
   light/dark theme and confirm the logo appears at the same height as the
   others with visually even spacing, on both themes and on a narrow (~390 px)
   viewport.

---

### SAP

- **File:** `sap-color.svg`
- **Source:** <https://commons.wikimedia.org/wiki/File:SAP_2011_logo.svg>
- **Fetched:** 2026-10-26
- **Licence:** Public domain (PD-textlogo / simple shapes)
- **Trademark owner:** SAP SE

### BWI (BWI GmbH)

- **File:** `bwi-color.svg`
- **Source:** <https://de.wikipedia.org/wiki/BWI_GmbH> (logo downloaded via Wikimedia Commons; no stable Commons file page found)
- **Fetched:** 2026-10-26
- **Licence:** Public domain (PD-textlogo)
- **Trademark owner:** BWI GmbH

### SAP NS2 (SAP National Security Services)

- **File:** `sap-ns2.png` (raster, 900×500)
- **Source:** <https://getlogovector.com/sap-ns2-logo-vector-svg/>
- **Fetched:** 2026-10-26
- **Licence:** Editorial use only (no explicit licence found)
- **Trademark owner:** SAP National Security Services, Inc.

### Gardener

- **File:** `gardener-color.svg`
- **Source:** <https://github.com/gardener/gardener/blob/master/logo/gardener.svg>
- **Trademark owner:** NeoNephos Foundation
- **Fetched:** 2026-10-26
- **Licence:** NeoNephos trademark usage guidelines
- **Notes:** viewBox cropped from `0 0 595.3 170.1` to `46 0 502 170.1` to
  remove ~24 px of internal padding on each side. No path data changed.

### Konfidence

- **File:** `konfidence-dark.svg`
- **Source:** <https://konfidence.cloud>
- **Trademark owner:** SAP SE / Konfidence project
- **Fetched:** 2026-10-26
- **Licence:** Editorial use (no explicit licence)
- **Notes:** viewBox cropped from `0 0 3440 400` to `78 0 3283 400` to
  remove ~80 px of internal padding on each side. No path data changed.

### Kyma

- **File:** `kyma-light.svg` / `kyma-dark.svg` (icon + label lockup built from `kyma-color.svg`)
- **Source:** <https://kyma-project.io/assets/logo_icon.svg>
- **Trademark owner:** SAP SE / Kyma project
- **Fetched:** 2026-10-26
- **Licence:** Editorial use (no explicit licence)
- **Notes:** viewBox cropped from `0 0 150 60` to `0 0 126 60` to remove
  ~24 px of internal padding on the right side. No path data changed.

### Open Control Plane (OCP)

- **File:** `ocp-light.svg` / `ocp-dark.svg` (icon + label lockup built from `ocp-color.svg`)
- **Source:** <https://open-control-plane.io>
- **Trademark owner:** NeoNephos Foundation
- **Fetched:** 2026-10-26
- **Licence:** NeoNephos trademark usage guidelines
- **Notes:** `ocp-dark.svg` remaps the original dark blues to lighter tones for
  dark-mode visibility; stroke colour switched from `#221f20` to `#f5f5f7`.
  viewBox cropped from `0 0 350 60` to `0 0 284 60` on both files to remove
  ~67 px of internal padding on the right side. No path data changed.

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
- **Notes:** Dark variant derived from the official light SVG by replacing the
  fill colour with white. viewBox cropped from
  `0 0 1612.0708246 546.8535461` to `111 0 1385 546.8535461` on both files to
  remove ~21 px of internal padding on each side. No path data changed.

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
