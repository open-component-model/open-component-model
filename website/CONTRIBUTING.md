# Contributing to OCM Documentation

This guide helps contributors create and place new documentation content according to the [Diataxis framework](https://diataxis.fr/).
These guidelines apply to **all new content** and do not reorganize existing documentation.

_NOTE: You may notice some inconsistencies with our current structure. You are encouraged to contribute improvements to this guide and
to the existing documentation to make it conform to this guideline._

## Table of Contents

- [Diataxis Overview](#diataxis-overview)
- [OCM Section Mapping](#ocm-section-mapping)
- [Content Templates](#content-templates)
- [Content Decision Flowchart](#content-decision-flowchart)
- [Repository Placement Guide](#repository-placement-guide)
- [Writing Checklists](#writing-checklists)
- [Examples](#examples)

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

Goal-oriented directions for specific tasks.

**Characteristics:**

- Assume reader knows OCM basics (completed Getting Started), reference as needed
- Use conditional structure: "If you want X, do Y"
- One focused task per guide
- Link to Reference for parameter details

**Example titles:**

- "(How to) Configure Private Registry Authentication"
- "(How to) Transfer Components Between Registries"

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

| Repository                                  | Status         | Components                              | Components                          |
|---------------------------------------------|----------------|-----------------------------------------|-------------------------------------|
| `open-component-model/ocm`                  | Current stable | CLI tool, Go library (v0.x)             | CLI tool, Go library (v0.x)         |
| `open-component-model/open-component-model` | Next-gen       | CLI, Go library, Kubernetes controllers | New library, Kubernetes controllers |

### Feature-Based Decision Tree

```text
What are you documenting?

CLI command/flag in old CLI (open-component-model/ocm)?
  -> ocm repo (content_versioned/version-legacy/docs/reference/)
  -> Auto-imported to website
CLI command/flag in new CLI (open-component-model/open-component-model/cli)?
  -> mono-repo (docs/reference/)
  -> Auto-imported to website

Go library function/type from (open-component-model/ocm)?
  -> Respective source repo documentation, available as go package documentation
Go library function/type from (open-component-model/open-component-model/bindings/go)?
  -> Respective source repo documentation, available as go package documentation

Kubernetes controller / CRD / Helm Chart?
  -> `open-component-model/kubernetes/controller` has CRD definitions and controller Helm Charts

A new way to start using OCM?
  -> ocm-website (getting-started/)
User workflow spanning multiple tools step-by-step?
  -> ocm-website (tutorials/)
A specific process or enablement of a concrete goal?
  -> ocm-website (how-to/)
Conceptual explanation of OCM?
  -> ocm-website (concepts/)
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

### How-to Guide Example (tutorials/)

**Good:** "How to Configure Private Registry Authentication"

### Explanation Example (concepts/)

**Good:** "Understanding Component Versions"

### Reference Example (reference/)

**Good:** "ocm transfer Command Reference"

---

## Additional Resources

- [Diataxis Framework](https://diataxis.fr/) - The documentation framework these guidelines follow
- [OCM Specification](https://ocm.software/spec/) - Authoritative OCM definitions
- [Hugo Documentation](https://gohugo.io/documentation/) - Site generator documentation
