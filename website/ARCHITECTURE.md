# Website Architecture

Reference for developers and AI agents. Dense, accurate, structured for quick lookup.

---

## 1. Theme Stack

Hugo site using the **Doks** documentation theme (`@thulite/doks-core` v1.9.3) built on Bootstrap.

Supporting modules:

| Module | Role |
|--------|------|
| `@thulite/images` | Image optimization |
| `@thulite/inline-svg` | SVG inlining |
| `@thulite/seo` | SEO metadata |
| `thulite` | Base styling |
| `@tabler/icons` | Icon library |
| `preact` v10.29.0 | Client-side rendering (schema renderer) |

**Module mount priority** (from `config/_default/module.toml`): Local files take precedence over theme
files. Hugo resolves templates in mount order — first match wins.

Layout mount priority:

1. `layouts/` (local overrides — highest priority)
2. `node_modules/@thulite/doks-core/layouts` (main theme, `home.html` explicitly excluded)
3. `node_modules/@thulite/core/layouts` (core components)
4. `node_modules/@thulite/seo/layouts`, `@thulite/images/layouts`, `@thulite/inline-svg/layouts`

Same pattern for assets: local `assets/` overrides theme assets.
`@tabler/icons/icons` mounted at `assets/svgs/tabler-icons`.

**How to customize**: Create a file at the same path under `layouts/` or `assets/` — it automatically
overrides the theme file. No config changes needed.

---

## 2. Layouts

### Base Templates (`layouts/_default/`)

**`baseof.html`** — Overrides `@thulite/doks-core` baseof.
Root HTML structure. Mermaid diagram support with theme-aware rendering (detects light/dark via
MutationObserver on `data-bs-theme`, re-renders diagrams on toggle). Homepage gets `.content` div
(full-width), regular pages get `.wrap` with container. Footer only on homepage.

**`single.html`** — Overrides `@thulite/doks-core` single.
Documentation page layout. Responsive 3-column: sidebar (left), content (center), TOC (right,
desktop only). Added: version banner partial, custom `render-section-menu.html` sidebar.

**`section.html`** — Overrides `@thulite/doks-core` section.
Section overview pages. Emoji-to-SVG icon mapping (21 emoji to SVG filename mappings), card grid
layout for child pages. Content authors use emoji in frontmatter `icon:` field, resolved to SVG at
render time.

**`home.html`** — Overrides `@thulite/doks-core` home (excluded from mount).
Completely custom landing page. Hero section with animated gradient text, CTA buttons. Action cards
grid (from `Params.actions`). Benefits cards grid (from `Params.benefits`). Icons loaded from SVG
resources.

### Render Hooks (`layouts/_default/_markup/`, `layouts/_markup/`)

**`_default/_markup/render-link.html`**
Custom link processing. External links get `target="_blank"`. `.md` links: strips extension,
converts underscores to dashes. OCM CLI command links (`ocm-*` pattern): placed in current section
directory. Enables markdown-style links in auto-generated CLI reference docs.

**`_markup/render-codeblock-mermaid.html`**
Converts fenced `mermaid` code blocks to `<pre class="mermaid">`. Sets `hasMermaid` page store flag
for conditional Mermaid JS loading in `baseof.html`.

### Header Partials (`layouts/_partials/header/`)

**`custom-header.html`** — Overrides `@thulite/doks-core` header.
Complete header/navbar. Version switcher dropdown (lists all content versions, marks current, shows
"(default)" badge), responsive mobile offcanvas menu, theme toggle, social links, search integration.

**`version-warning.html`** — New (not in theme).
Small "Legacy" badge shown only when viewing legacy version docs. Styled with `.version-droplet`.

### Main Content Partials (`layouts/_partials/main/`)

**`version-banner.html`**
Full-width warning banner on non-default version pages. "You are viewing documentation for an older
OCM version" with link to latest.

**`preview-banner.html`**
Yellow warning banner for draft/scheduled content. Shows "Draft", "Scheduled", or "Draft & Scheduled"
with date.

**`preview-badge.html`**
Small inline badge (DRAFT=yellow, SCHEDULED=cyan) for blog listing pages.

### Other Partials

**`head/custom-head.html`** — Overrides `@thulite/doks-core`.
Adds RSS feed `<link>` for blog section.

**`head/libsass.html`** — Overrides `@thulite/doks-core`.
Sass build pipeline. Dev: source maps + Dart Sass. Prod: compressed + PostCSS + SRI integrity hash +
fingerprinting.

**`footer/footer.html`** — Overrides `@thulite/doks-core`.
Homepage footer. EU/German government funding logos + disclaimer, Neonephos branding, Linux Foundation
Europe copyright, Netlify build credit with git commit hash.

**`footer/script-footer-custom.html`** — Hook point.
Empty placeholder for custom footer scripts.

**`sidebar/render-section-menu.html`** — Overrides `@thulite/doks-core`.
Sidebar navigation. Key change: section titles are clickable `<a>` links (Doks default only makes
them toggle expand/collapse). Recursive tree walker. Respects `sidebar.collapsed` frontmatter.
`aria-current` for active/ancestor highlighting.

### Shortcodes

**`_shortcodes/callout.html`**
Info/warning/note/danger callout boxes. Parameters: `context` (note/info/warning/danger), `title`,
`icon`. Icon inlined as SVG. Markdown content supported.

**`_shortcodes/schema-renderer.html`**
Server-side shortcode rendering a Preact-based schema viewer. Required param: `url`. Builds
`assets/js/schema/schema-renderer.tsx` with Preact JSX config. Version-aware: prepends version name
to schema URL.

**`shortcodes/person-card.html`**
Contributor profile cards. Required: `name`, `role`, `github`, `profile` (OpenProfile). Avatar from
`github.com/<user>.png`. Links to OpenProfile page.

### Blog Templates (`layouts/blog/`)

**`single.html`**
Blog post. Featured image (searches for `*feature*`, `*cover*`, `*thumbnail*` resources). Tags as
buttons. Related posts section (Hugo's related content, max 3). Preview banner for drafts.

**`list.html`**
Blog archive. Paginated card grid with featured images, preview badges, metadata. Optional author
avatar.

---

## 3. SCSS Architecture

All styles in `assets/scss/`. Import orchestration via `common/_custom.scss` which imports all other
files in cascade order.

### Brand & Variables (`common/_variables-custom.scss`)

The foundation — defines the entire color system as CSS custom properties on `:root` with
`[data-bs-theme="dark"]` and `[data-bs-theme="light"]` variants.

Key brand colors:

| Variable | Value |
|----------|-------|
| `--brand-blue-dark` | `#257ddc` |
| `--brand-blue-mid` | `#1d65b4` |
| `--brand-cyan` | `#4cc9f0` |
| `--brand-gradient` | `linear-gradient(to right, var(--brand-cyan), #4361ee)` |

Overrides Doks's green palette by mapping `--lp-c-green-*` variables to OCM blue. Also defines
variables for: card system (backgrounds, borders, hover states, glow), navigation (height 56px, gaps,
font weights), buttons, hero section, sidebar active/hover colors, TOC/description box colors.

### File Reference

**`common/_base.scss`**
Inter font import (Google Fonts), SVG `color-scheme` for dark mode, container layouts, sticky header
offset (`padding-top: var(--nav-height)`).

**`common/_branding.scss`**
Quicksand font for logo text (300 weight), SVG logo color forcing (white in header), theme-dependent
text colors.

**`common/_header-custom.scss`**
Sticky header with glassmorphism (`backdrop-filter: blur(12px) saturate(140%)`), navbar responsive
layout, version droplet badge (yellow #ffc107), offcanvas mobile menu, GitHub pill, dropdown styling.

**`common/_sidebar.scss`**
Sidebar link sizing (sections 1.05rem, leaves 0.95rem), active state bold (600 weight), hover color
(#233284), width constraint (260-360px clamp), TOC font hierarchy (0.95 to 0.9 to 0.875rem).

**`common/_cards.scss`**
Unified `.ocm-card` system with 3 variants (action, benefit, vertical). Mouse-tracking radial
gradient glow effect via `var(--mouse-x)`, `var(--mouse-y)`. Responsive grids (2 to 3 to 4 to 5
columns). Hover: translateY + scale + shadow. Person cards (160px blocks, 80x80 avatars).

**`common/_components.scss`**
TOC box styling (`.toc-box`), section description box (`.section-desc-box`), details/summary
elements. Light/dark variants via CSS variables.

**`common/_pages.scss`**
Page-specific layouts. H1-H6 heading scale (2.5 to 1rem). Schema reference pages: hide TOC,
full-width content. Community/Roadmap: hide sidebars. Docs content compact spacing.

**`common/_landing-ord.scss`**
Landing page hero: text gradients (`-webkit-background-clip: text`), responsive sizing (48 to 56 to
64px), hero image glow effects. Footer: EU funding section, Neonephos logos, Netlify credit.
Light/dark footer variants.

**`common/_version-warning.scss`**
Version warning banner: yellow background (#fffbe6), dark variant (#fff3cd).

**`common/_forms-dart-sass-compat.scss`**
Dart Sass compatibility: redeclares Bootstrap form rules with `!optional` to fix `@extend` errors
from Bootstrap 4 to 5 migration.

**`components/_forms.scss`**
Proxy: forwards to `_forms-dart-sass-compat.scss`.

**`components/_schema-renderer.scss`**
Schema reference interactive table: type badges (string/number/boolean/object/array with colors),
depth indentation with visual borders, expand/collapse buttons, skeleton loading animation, responsive
mobile layout.

### Dark/Light Theme

All custom styles use CSS variables defined in `_variables-custom.scss`. Theme switching is handled
by Bootstrap's `[data-bs-theme]` attribute on `<html>`. Doks's theme toggle button changes this
attribute; the MutationObserver in `baseof.html` re-renders Mermaid diagrams on change.

---

## 4. Multi-Version System

Content versioning uses Hugo module mounts to serve different content folders for different versions.

**How it works** (from `module.toml`):

- `content/` serves as "latest" version
- `content_versioned/version-legacy/` serves as "legacy" version
- `content/blog/` shared across all versions (mounted separately)
- New versions: `npm run cutoff -- x.y.z` copies `content/` to `content_versioned/version-x.y.z/`
  and updates `hugo.toml` + `module.toml`

**Version UI components:**

| Component | File | Behavior |
|-----------|------|----------|
| Version switcher | `custom-header.html` | Dropdown lists all versions, marks current + default |
| Legacy badge | `version-warning.html` | "Legacy" indicator in header on legacy version |
| Warning banner | `version-banner.html` | Full-width banner on non-default pages with link to latest |

**Default version**: Configured in `hugo.toml` as `defaultContentVersion = "latest"`. The cutoff
script can update this with `--keepDefault` flag to preserve "latest" as default.

---

## 5. External Content Imports

Hugo module imports in `module.toml` pull content and schemas from OCM sub-projects:

| Source Module | Mount Target | Content |
|--------------|-------------|---------|
| `cli` | `content/docs/reference/ocm-cli` | CLI reference docs (auto-generated) |
| `bindings/go/constructor` | `static/latest/schemas/bindings/go/constructor` | Constructor JSON schemas |
| `bindings/go/descriptor/v2` | `static/latest/schemas/bindings/go/descriptor/v2` | Descriptor v2 JSON schemas |
| `kubernetes/controller` | `static/latest/schemas/kubernetes/controller` | CRD YAML files |

All imports are pinned to `version: "latest"` and scoped to the "latest" content version.

---

## 6. JavaScript

**`assets/js/custom.js`**
Sidebar UX fix: intercepts clicks on section navigation links (`.section-nav details > summary
a.docs-link`). Prevents native `<details>` toggle when navigating to a section page (avoids visual
flash where sidebar collapses then re-expands on navigation). Toggle only fires when clicking the
link of the already-active page. Uses capturing phase event listener.

**`assets/js/schema/schema-renderer.tsx`**
Preact component for interactive JSON schema visualization. Built by Hugo's js.Build with Preact JSX
config. Loaded by `schema-renderer.html` shortcode.

---

## 7. Asset Pipeline

### Sass Compilation

Configured in `_partials/head/libsass.html`:

- **Compiler**: Dart Sass (via `sass-embedded` npm package)
- **Development**: source maps enabled, `node_modules/` include path, deprecation warnings silenced
- **Production**: compressed output, PostCSS (autoprefixer etc.), fingerprinted filename, SRI
  integrity hash, PostProcess optimization
- **Entry point**: theme's main SCSS which `@use`/`@import`s the custom files via module mounts

### Hugo js.Build (Schema Renderer)

- **Target**: ES2020
- **JSX**: automatic with Preact as import source
- **Production**: minified, no source maps
- **Development**: source maps, no minification

### Image Processing (Hugo Config)

- **Quality**: 85, Lanczos filter
- **LQIP** (low-quality placeholder): "16x webp q20"
- **Loading**: lazy by default
- **Widths**: 480, 576, 768, 1025, 1200, 1440
