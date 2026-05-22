---
title: "Shipping AI Agent Skills with the Open Component Model"
date: 2026-05-22T10:00:00+02:00
draft: false
description: "A vendor-neutral, airgap-capable approach to versioning, signing, and distributing AI agent skills using OCI registries — no lock-in, no proprietary store."
summary: "AI coding agents need skills to work well with your stack. Today those skills live in dotfiles with no versioning, no provenance, and no shared distribution mechanism. OCM fixes that."
categories: ["ai", "cli"]
tags: ["ai", "skills", "agent", "oci", "supply-chain"]
contributors: []
---

AI coding agents are proliferating fast. Claude Code, OpenAI Codex CLI, Gemini CLI, Cursor, Continue — each has its own way of learning how to work with your stack. They call the configuration different things: skills, rules, context files, prompts. But the underlying problem is the same everywhere.

**Nobody has solved distribution.**

## The Problem is Bigger Than One Agent

Wherever you look, agent configuration lives in dotfiles:

- Claude Code: `~/.claude/skills/<name>/SKILL.md`
- OpenAI Codex CLI: `~/.agents/skills/<name>/SKILL.md`
- Cursor: `.cursorrules`
- Continue: `.continue/`

Sharing this configuration today means copying files by hand, committing them to a personal dotfiles repo, or describing them in a README and hoping a teammate bothers to recreate them. There is no versioning, no provenance, no way to express "install the `golang-patterns` skill at version 1.2.0 from my organisation's internal registry."

Every team that builds up a useful library of agent configuration faces the same distribution gap. The files exist. The knowledge exists. But the moment you want to share it reliably across machines, teams, or air-gapped environments — the tooling runs out.

## A Vendor-Neutral, Lock-In-Free Answer

The [Open Component Model](https://ocm.software) is an open standard for describing and distributing software artifacts. It is not tied to any cloud provider, any agent vendor, or any proprietary store. Its transport layer is the OCI distribution spec — the same protocol used by every container registry on the planet. GitHub Container Registry, AWS ECR, Google Artifact Registry, your on-premises Zot instance — they all work without modification.

The core abstraction is simple: a *component version* is a named, versioned bundle of *resources*. Resources can be anything — container images, Helm charts, binaries, plain text files. The component descriptor records what is in the bundle, how to retrieve it, and who produced it.

That abstraction fits agent skills exactly. A skill is a text file with a name and a version. An OCI registry is a fine place to store a catalogue of them, and OCM adds the layer that makes it safe:

- **Signing** — component versions can be signed with cosign or GPG. You can verify a skill came from your organisation, not an arbitrary source.
- **Air-gap transport** — OCM's CTF (Common Transport Format) is a portable local archive. You can carry a skill catalogue on a USB drive, mirror it to an internal registry, or ship it as part of a software delivery pipeline with no internet access required.
- **Composability** — nothing stops you from placing a skill alongside the container image and Helm chart for the same service, all in one component version. Your deployment unit and your agent configuration travel together.

No proprietary store. No agent-specific distribution format. No cloud dependency. Just OCI.

## One Component, Many Skills

The key design decision: **one OCM component = one catalogue release**. Each skill is a *resource* inside that component, identified by name and the type label `ai.skill/v1`:

```text
Component: myorg.io/ai-skill-catalogue   version: 1.0.0
├── resource: ocm-guide        type: ai.skill/v1   (SKILL.md)
├── resource: golang-patterns   type: ai.skill/v1   (SKILL.md)
└── resource: backend-patterns  type: ai.skill/v1   (SKILL.md)
```

`ai.skill/v1` is just a string label — OCM stores unknown resource types as blobs. No plugins, no registry-side changes, no coordination with any vendor required.

## What a Skill Looks Like

A skill is a Markdown file with a YAML frontmatter header. The agent reads `name` and `description` to decide when to apply it:

```markdown
---
name: golang-patterns
description: Idiomatic Go patterns and best practices. Use when writing or reviewing Go code.
tools: ["Read", "Bash"]
---

# Go Patterns

## Error handling
Always wrap errors with context...
```

The format is readable by humans, writable by hand, and parseable by any agent that follows the convention. OCM does not care about the content — it stores and retrieves whatever bytes you put in.

## Hands-On: Package and Install Skills

### 1. Organise your skills directory

```text
my-skills/
├── ocm-guide/
│   └── SKILL.md
└── golang-patterns/
    └── SKILL.md
```

### 2. Generate the component constructor

```bash
ocm skill push ./my-skills \
  --component myorg.io/ai-skill-catalogue \
  --version 1.0.0 \
  --output constructor.yaml
```

This scans `./my-skills` for `SKILL.md` files and writes a `constructor.yaml` that describes the component:

```yaml
components:
  - name: myorg.io/ai-skill-catalogue
    version: 1.0.0
    provider:
      name: myorg.io
    resources:
      - name: ocm-guide
        type: ai.skill/v1
        version: 1.0.0
        input:
          type: file
          path: /absolute/path/to/my-skills/ocm-guide/SKILL.md
      - name: golang-patterns
        type: ai.skill/v1
        version: 1.0.0
        input:
          type: file
          path: /absolute/path/to/my-skills/golang-patterns/SKILL.md
```

### 3. Package into a local archive

OCM's CTF archive requires no registry to get started:

```bash
ocm add component-version \
  --repository ./my-catalogue \
  --constructor constructor.yaml
```

Output:

```text
 COMPONENT                    │ VERSION │ PROVIDER
─────────────────────────────┼─────────┼──────────
 myorg.io/ai-skill-catalogue  │ 1.0.0   │ myorg.io
```

### 4. Install skills

Pull a single skill for **Claude Code** (default):

```bash
ocm skill pull ./my-catalogue//myorg.io/ai-skill-catalogue:1.0.0 \
  --skill ocm-guide
```

The skill lands at `~/.claude/skills/ocm-guide/SKILL.md`.

Install for **OpenAI Codex CLI** instead:

```bash
ocm skill pull ./my-catalogue//myorg.io/ai-skill-catalogue:1.0.0 \
  --skill ocm-guide \
  --target codex
```

The skill lands at `~/.agents/skills/ocm-guide/SKILL.md`.

Install for **both agents at once**:

```bash
ocm skill pull ./my-catalogue//myorg.io/ai-skill-catalogue:1.0.0 \
  --target all
```

Pull every skill in the catalogue in one shot:

```bash
ocm skill pull ./my-catalogue//myorg.io/ai-skill-catalogue:1.0.0
```

Output:

```text
skill installed   skill=ocm-guide       output=~/.claude/skills/ocm-guide/SKILL.md
skill installed   skill=golang-patterns  output=~/.claude/skills/golang-patterns/SKILL.md
```

## Using a Real OCI Registry

Swap the local archive path for any OCI registry URL when you are ready to share across machines or teams:

```bash
# push to GHCR
ocm add component-version \
  --repository ghcr.io/myorg \
  --constructor constructor.yaml

# anyone on the team installs
ocm skill pull ghcr.io/myorg//myorg.io/ai-skill-catalogue:1.0.0
```

Authentication uses standard OCI credential helpers — `docker login ghcr.io` is sufficient. For air-gapped environments, mirror the CTF archive to your internal registry with `ocm transfer` and point the pull command there.

## The Self-Referential Part

The first skill we packaged in this catalogue is `ocm-guide` — a skill that teaches agents how OCM works: the component descriptor format, resource types, CLI patterns, the plugin system. It is distributed via OCM, and it explains OCM.

Once installed, ask your agent to help write a component constructor, understand a resource access error, or navigate the plugin system — and it answers from the skill's context rather than general training data.

## Why This Approach Matters

The skills distribution problem is not unique to one agent or one company. It is a structural gap in the agentic tooling ecosystem: useful configuration accumulates, but there is no standard way to version it, sign it, ship it, or consume it across teams and environments.

OCM provides that standard. It was designed for exactly this pattern — versioned, signed, portable software artifacts distributed over OCI. The fact that it already works for container images and Helm charts means the infrastructure is already in place. A skill catalogue is just another OCI artifact.

The properties you get are the same ones that matter for any supply chain artefact:

- **Versioning** — `1.0.0`, `1.1.0`, semantic versioning, floating tags.
- **Provenance** — signing lets you verify a skill came from your organisation.
- **Portability** — CTF archives work completely offline.
- **Vendor neutrality** — the same catalogue works for Claude Code today and whatever agent your team adopts next year.

No lock-in. No proprietary store. Skills travel with your software.

## Full Command Reference

```bash
# Generate constructor from a skills directory
ocm skill push <skills-dir> \
  --component <domain>/<name> \
  --version <semver> \
  [--provider <name>]       # defaults to domain part of component name
  [--output <file>]         # write YAML to file instead of stdout

# Pull skills from a catalogue
ocm skill pull <repo>//<component>:<version> \
  [--skill <name>]          # omit to pull all ai.skill/v1 resources
  [--output <path>]         # custom output path (requires --skill)
  [--target <agent>]        # claude (default), codex, or all
```

Repository reference formats:

- Local CTF: `./my-catalogue`
- OCI registry: `ghcr.io/myorg`
- Explicit type: `CTF::./my-catalogue` or `OCI::ghcr.io/myorg`
