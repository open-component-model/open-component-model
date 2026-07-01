# Contributing to the OCM Website

This guide covers development on the OCM project website in `website/`. For the general contribution process, see the
[central contributing guide](https://ocm.software/community/contributing/).

## Overview

The website is built with [Hugo](https://gohugo.io/) using the [Thulite/Doks](https://getdoks.org/) theme and hosted
at [ocm.software](https://ocm.software). Documentation follows the [Diataxis framework](https://diataxis.fr/), which
organizes content into tutorials, how-to guides, explanations, and reference material.

## Prerequisites

- **Node.js** and **npm** (see version requirements in `engines` in `package.json`)
- Hugo is installed automatically via the `hugo-extended` npm package - you do not need to install it separately

## Local Development

```bash
# Install dependencies (includes Hugo)
cd website
npm install

# Start the development server
npm run dev

# Start with draft content visible
npm run dev:drafts

# Build the site
npm run build
```

The dev server runs at `http://localhost:1313` with live reload.

## Linting

Markdown linting is run from the repository root using the shared tooling:

```bash
# Lint all Markdown files across the repo (from the repository root)
task tools:markdownlint

# Lint with auto-fix
task tools:markdownlint -- --fix
```

The website also has ESLint configured for JavaScript:

```bash
cd website
npm run lint:scripts

# Lint and auto-fix
npm run lint:scripts:fix
```

## Theme overlays

The website builds on the [@thulite/doks-core](https://www.npmjs.com/package/@thulite/doks-core) Hugo theme. Some
templates and shortcodes from the theme are *shadowed* — a file with the same path under `website/layouts/` overrides
the theme's version. Hugo's template lookup picks the project file first, so the theme's original is never rendered.

Shadowing is the supported way to customize Doks. The cost is **drift**: when the theme is upgraded, the upstream
file may change in ways that are not reflected in our shadow, and Hugo gives no warning.

### Tagging a shadow

Every file under `website/layouts/` that is derived from `@thulite/doks-core` must declare its baseline upstream
version with a Hugo comment near the top of the file:

```html
{{- /*
  Based on: @thulite/doks-core@<version> (path: <upstream-relative-path>)
  See website/CONTRIBUTING.md "Theme overlays" for the audit/update process.
*/ -}}
```

For example:

```html
{{- /*
  Based on: @thulite/doks-core@1.9.3 (path: layouts/_shortcodes/callout.html)
  ...
*/ -}}
```

Files **without** this tag are treated as project-original (no upstream counterpart) and are not audited. If you add
a new shadow, you must add the tag — otherwise drift on that file goes unnoticed.

If the upstream file itself credits an even earlier source (e.g. Doks adapted its `callout` shortcode from Bootstrap),
preserve that attribution as a separate `Originally adapted from: <url>` line below the `Based on:` tag.

### Auditing

The audit script compares each tagged shadow against the upstream file at both the *recorded* version (from the tag)
and the *installed* version (from `node_modules/@thulite/doks-core/package.json`):

```bash
cd website
npm run audit:overlays            # report only files that need attention
npm run audit:overlays -- -v      # also show files that are clean
```

For each shadow it reports one of:

| Status      | Meaning                                                                                                    | Action                                                                  |
|-------------|------------------------------------------------------------------------------------------------------------|-------------------------------------------------------------------------|
| `OK`        | Recorded baseline matches the installed upstream byte-for-byte.                                            | None.                                                                   |
| `STALE_TAG` | Recorded version differs from installed, but upstream content is unchanged at both versions.               | Bump the version in the tag.                                            |
| `DRIFT`     | Upstream file changed between the recorded version and the installed version.                              | Review the diff. Port any improvements into the shadow, then bump tag.  |
| `REMOVED`   | Upstream file no longer exists at the installed version.                                                   | The shadow may be obsolete or moved upstream — investigate and resolve. |
| `ERROR`     | The tag is malformed, the upstream tarball cannot be fetched, or the recorded path doesn't exist upstream. | Fix the tag.                                                            |

Exit code is non-zero if any shadow is in `DRIFT`, `REMOVED`, or `ERROR`. `OK` and `STALE_TAG` are informational.

### When to run the audit

- **Before bumping `@thulite/doks-core`**: run the audit on the current state to confirm a clean baseline.
- **After bumping `@thulite/doks-core`**: run again. Any new `DRIFT` reports show what changed upstream that we may
  need to port. Any `REMOVED` reports show files that disappeared from upstream — decide whether the shadow is still
  useful.
- **When adding a new shadow**: tag it with the currently installed version. The audit then tracks it on future
  bumps.

### Pinning the theme version

`package.json` should pin `@thulite/doks-core` to an exact version (`"1.9.3"`, not `"^1.9.3"`). Floating versions
turn theme upgrades into surprise drift; pinning makes upgrades deliberate.

## Content Authoring Guide

The rest of this document covers how to create and place documentation content. All new content should follow the
[Diataxis framework](https://diataxis.fr/). You may notice some inconsistencies with the current structure - improvements
are welcome.

---

## Diataxis Overview

Diataxis organizes documentation into four types based on user needs:

| Type              | User Need            | Characteristics                           |
|-------------------|----------------------|-------------------------------------------|
| **Tutorials**     | "Help me learn"      | Learning-oriented, guided, step-by-step   |
| **How-to Guides** | "Help me do X"       | Task-oriented, assumes competence         |
| **Explanation**   | "Help me understand" | Understanding-oriented, discusses "why"   |
| **Reference**     | "Give me facts"      | Information-oriented, describes machinery |

Each type serves a distinct purpose. Mixing types within a single document confuses readers and reduces effectiveness.

---

## OCM Section Mapping

| Diataxis Type | OCM Website Section             | Purpose                                                    |
|---------------|---------------------------------|------------------------------------------------------------|
| Tutorials     | `content/docs/getting-started/` | Guide newcomers through chosen simple learning experiences |
| Tutorials     | `content/docs/tutorials/`       | Provide in-depth tutorials for integration with OCM        |
| Explanation   | `content/docs/overview/`        | Provide Project Level Guidance                             |
| Explanation   | `content/docs/concepts/`        | Explain design decisions and rationale                     |
| How-to Guides | `content/docs/how-to/`          | Provide task-oriented directions                           |
| Reference     | `content/docs/reference/`       | Document technical specifications                          |

### Explanation (`content/docs/concepts/`)

Explain the "why" behind OCM design decisions.

**Characteristics:**

- Provide context, rationale, and connections
- Can include opinions and trade-off discussions
- No step-by-step instructions
- Link to OCM Specification for authoritative definitions

**Example titles:**

- "Understanding Component Versions"
- "The OCM Security Model"

### Tutorials (`content/docs/getting-started/` & `content/docs/tutorials/`)

Guide newcomers through complete learning experiences. Use the [Tutorial Template](./content_templates/template-tutorial.md) to ensure consistency.

#### Getting Started (`getting-started/`)

Guide newcomers through complete learning experiences.

**Characteristics:**

- Every step produces a visible, verifiable result
- Show destination upfront ("In this tutorial you will...")
- Avoid explanation digressions - link to Concepts or how to instead
- Perfect reliability: every command must work exactly as written

**Example titles:**

- "Create Your First Component Version"

#### Other Tutorials (`tutorials/`)

In-depth tutorials that explore advanced topics and real-world scenarios.

**Characteristics:**

- Assume reader completed Getting Started tutorials
- Guide through complex, multi-step workflows (e.g., signing, credential resolution, bootstrap deployments)
- Show how different OCM features work together
- Every step produces a visible, verifiable result
- Link to Concepts for "why" questions and Reference for parameter details

**Example titles:**

- "Credential Resolution in OCM"
- "Deploy Helm Charts with Bootstrap Setup"

### Reference (`reference/`)

Factual, authoritative technical descriptions.

**Characteristics:**

- Structure mirrors the product (CLI commands, CRD fields, etc.)
- Include usage examples, not tutorials
- Auto-generated from source repositories where possible

**Example content:**

- CLI command reference (imported via Hugo module)
- Configuration schema documentation
- CRD field specifications

### How-to Guides (`content/docs/how-to/`)

Task-oriented directions for accomplishing specific goals.

**Characteristics:**

- Assume reader has completed Getting Started and understands OCM basics
- Focus on one task per guide
- Use conditional structure where appropriate ("If you want X, do Y")
- Link to Reference for parameter details

**Example titles:**

- "How to Configure Private Registry Authentication"
- "How to Transfer Components Between Registries"

---

## Content Templates

To help you get started with writing documentation, we provide templates for each content type:

- **[Tutorial Template](./content_templates/template-tutorial.md)**
  - Complete guide for creating learning-oriented tutorials
  - Includes examples for Mermaid diagrams, `{{< steps >}}`, `{{< tabs >}}`, and `{{< details >}}` shortcodes
  - Shows how to structure prerequisites, scenarios, and success indicators
  - Provides checklist for publication readiness

- **[How-to Template](./content_templates/template-how-to.md)**
  - Task-focused guide template
  - Demonstrates goal-oriented structure
  - Shows troubleshooting format with symptom-cause-fix
  - Includes examples for `{{< tabs >}}` and `{{< card-grid >}}` shortcodes

- **[Concept Template](./content_templates/template-concept.md)**
  - Explanation-oriented template for design decisions and rationale
  - Focuses on "why" rather than "how"

These templates include inline comments and examples to guide you through creating high-quality documentation that follows Diataxis principles.

---

## Content Decision Flowchart

Use this flowchart to determine where new content belongs when contributing to the website:

```text
New documentation content?
         |
         v
Is it auto-generated from code?
  YES -> Source repo, imported via Hugo module
  NO  -> Continue
         |
         v
Are you teaching someone new to OCM how to use it?
  YES -> TUTORIAL -> getting-started/
  NO  -> Continue
         |
         v
Are you guiding a user through a process / workflow?
  YES -> TUTORIAL -> tutorials/
  NO  -> Continue
         |
         v
Are you helping accomplish a specific task/goal?
  YES -> HOW-TO -> how-to/
  NO  -> Continue
         |
         v
Are you explaining why/how OCM works?
  YES -> EXPLANATION -> concepts/
  NO  -> Continue
         |
         v
Are you describing machinery/syntax/options?
  YES -> REFERENCE -> reference/
```

---

## Repository Placement Guide

Documentation lives in different repositories depending on what it documents.

### Source Repositories

| Repository                                  | Status     | Components                              |
|---------------------------------------------|------------|-----------------------------------------|
| `open-component-model/open-component-model` | Active     | CLI, Go library, Kubernetes controllers |
| `open-component-model/ocm`                  | Legacy     | CLI tool, Go library (v0.x)             |

### Feature-Based Decision Tree

```text
What are you documenting?

CLI command/flag in the legacy CLI (open-component-model/ocm)?
  -> website/content_versioned/version-legacy/docs/reference/
CLI command/flag in the current CLI?
  -> cli/docs/reference/ (Hugo mounts this into the website automatically)

Go library function/type?
  -> Source repo documentation, available as Go package documentation

Kubernetes controller / CRD / Helm Chart?
  -> kubernetes/controller/ has CRD definitions and controller Helm Charts

A new way to start using OCM?
  -> website/content/docs/getting-started/
User workflow spanning multiple tools step-by-step?
  -> website/content/docs/tutorials/
A specific process or enablement of a concrete goal?
  -> website/content/docs/how-to/
Conceptual explanation of OCM?
  -> website/content/docs/concepts/
```

### Marking Version-Specific Content

When content applies to a specific version or repository, add a callout:

```markdown
{{<callout context="note" title="" icon="">}}
This guide applies to OCM CLI v0.x. See [link] for the new library.
{{</callout>}}
```

```markdown
{{<callout context="note" title="" icon="">}}
This feature requires the new OCM library from `open-component-model/open-component-model`.
{{</callout>}}
```

You can find appropriate icons on this [website](https://tabler.io/icons).

---

## Writing Checklists

### Tutorial Checklist

Use the [Tutorial Template](./content_templates/template-tutorial.md) as a starting point.

- [ ] Title describes what learner will accomplish
- [ ] Prerequisites section lists all requirements
- [ ] Steps are numbered and sequential
- [ ] Every command can be copy-pasted and works
- [ ] Expected output shown after commands
- [ ] Final result is visible and verifiable
- [ ] Links to Concepts for "why" questions

### How-to Guide Checklist

Use the [How-to Template](./content_templates/template-how-to.md) as a starting point.

- [ ] Title starts with "How to..." or action verb
- [ ] States the goal in the first paragraph
- [ ] Assumes reader completed Getting Started
- [ ] Uses conditional structure where appropriate
- [ ] Focuses on one task only
- [ ] Links to Reference for parameter details

### Explanation Checklist

- [ ] Title describes the concept
- [ ] Explains "why" not just "what"
- [ ] Provides context and connections to other concepts
- [ ] Links to OCM Specification for definitions
- [ ] No step-by-step instructions
- [ ] Can be read away from the keyboard

### Reference Checklist

- [ ] Structure mirrors the product
- [ ] All parameters/options documented
- [ ] Default values stated
- [ ] Types and constraints specified
- [ ] Examples show usage (not tutorials)
- [ ] Consistent formatting throughout

---

## Examples

### Tutorial Example (getting-started/)

**Good:** "Create Your First Component Version"

### Tutorial Example (tutorials/)

**Good:** "Deploy Helm Charts with Bootstrap Setup"

### How-to Guide Example (how-to/)

**Good:** "How to Configure Private Registry Authentication"

### Explanation Example (concepts/)

**Good:** "Understanding Component Versions"

### Reference Example (reference/)

**Good:** "ocm transfer Command Reference"

---

## Additional Resources

- [Diataxis Framework](https://diataxis.fr/) - The documentation framework these guidelines follow
- [OCM Specification](https://github.com/open-component-model/ocm-spec) - Authoritative OCM definitions
- [Hugo Documentation](https://gohugo.io/documentation/) - Site generator documentation
