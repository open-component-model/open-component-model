# Shipping AI Skills with the Open Component Model

Imagine installing a Claude Code skill the same way you deploy a container image — versioned, signed, pulled from a registry. That's what we built, and this post walks through how.

## The Problem: Skills Live in Dotfiles

Claude Code skills are Markdown files. They teach the AI how to work with your stack — how to write idiomatic Go, how to query ClickHouse, how to structure a Django migration. They're genuinely useful, and they accumulate fast.

The problem is distribution. Right now, skills live in `~/.claude/skills/`. Sharing them means copying files, committing them to a dotfiles repo, or just describing them in a README and hoping someone bothers to recreate them. There's no versioning, no provenance, no way to say "install skill `golang-patterns` version 1.2.0 from my team's registry."

We wanted something better. And it turns out the Open Component Model was already built for exactly this kind of problem.

## What OCM Gives You

The [Open Component Model](https://ocm.software) is an open standard for describing and distributing software artifacts. Its core idea is simple: a *component version* is a named, versioned bundle of *resources*. Resources can be anything — container images, Helm charts, binaries, plain text files. The component descriptor records what's in the bundle, how to get it, and who produced it. Everything lives in a standard OCI registry.

That description fits AI skills perfectly. A skill is a text file with a name and a version. An OCI registry is a fine place to store a catalogue of them.

## One Component, Many Skills

The key design decision: **one OCM component = one catalogue release**. We don't create a separate component per skill — that would make the registry noisy and installs awkward. Instead, each skill is a *resource* inside a single catalogue component:

```
Component: jakob.io/ai-skill-catalogue   version: 1.0.0
├── resource: ocm-guide        type: ai.skill/v1   (SKILL.md)
├── resource: golang-patterns   type: ai.skill/v1   (SKILL.md)
└── resource: backend-patterns  type: ai.skill/v1   (SKILL.md)
```

The type `ai.skill/v1` is just a string label — OCM stores unknown resource types as blobs. No plugins, no new code on the registry side.

## What a Skill Looks Like

A skill is a Markdown file with a YAML frontmatter header. Claude Code reads the `name` and `description` to decide when to apply it, and follows the body as instructions:

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

Skills live at `~/.claude/skills/<skill-name>/SKILL.md`. Claude Code picks them up automatically on the next session — no restart needed.

## Hands-On: Package and Install Skills

### 1. Organise your skills directory

Your skills directory should have one subdirectory per skill, each containing a `SKILL.md`:

```
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

This scans `./my-skills` for `SKILL.md` files and writes a `constructor.yaml`:

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

OCM's CTF (Common Transport Format) is a local directory-based archive — no registry required to get started:

```bash
ocm add component-version \
  --repository ./my-catalogue \
  --constructor constructor.yaml
```

Output:
```
 COMPONENT                    │ VERSION │ PROVIDER
─────────────────────────────┼─────────┼──────────
 myorg.io/ai-skill-catalogue  │ 1.0.0   │ myorg.io
```

### 4. Install a skill into Claude Code

Pull a single skill directly into `~/.claude/skills/`:

```bash
ocm skill pull ./my-catalogue//myorg.io/ai-skill-catalogue:1.0.0 \
  --skill ocm-guide
```

The skill lands at `~/.claude/skills/ocm-guide/SKILL.md`. Claude Code picks it up in the next session.

To pull every skill in the catalogue at once, omit `--skill`:

```bash
ocm skill pull ./my-catalogue//myorg.io/ai-skill-catalogue:1.0.0
```

Output:
```
skill installed   skill=ocm-guide       output=~/.claude/skills/ocm-guide/SKILL.md
skill installed   skill=golang-patterns  output=~/.claude/skills/golang-patterns/SKILL.md
```

### 5. Verify it's active

List your installed skills:

```bash
ls ~/.claude/skills/
```

Or start a Claude Code session and ask: *"What skills do you have available?"* — it will list all installed skills including their descriptions.

## Using a Real OCI Registry

Once you're ready to share with a team, swap the local archive path for a registry URL. OCM uses the same `//component:version` syntax:

```bash
# push to GHCR
ocm add component-version \
  --repository ghcr.io/myorg \
  --constructor constructor.yaml

# anyone on the team installs
ocm skill pull ghcr.io/myorg//myorg.io/ai-skill-catalogue:1.0.0
```

Authentication follows standard OCI credential helpers — `docker login ghcr.io` is sufficient.

## The Self-Referential Part

The first skill we packaged in this catalogue is `ocm-guide` — a skill that teaches Claude how OCM works: the component descriptor format, resource types, CLI patterns, the plugin system. It's distributed via OCM, and it explains OCM.

Once installed, ask Claude to help you write a component constructor, understand a resource access error, or navigate the plugin system — and it answers from the skill's context rather than general training data.

## Why This Matters

OCI registries are infrastructure you already have. GitHub Container Registry, AWS ECR, Google Artifact Registry — they all speak the OCI distribution spec that OCM builds on. A skill catalogue is just another OCI artifact living alongside your container images. You get:

- **Versioning** — `1.0.0`, `1.1.0`, semantic versioning, floating tags.
- **Provenance** — OCM supports signing. You can verify a skill came from your team's registry, not a random source.
- **Transport** — CTF lets you air-gap the catalogue, carry it on a USB drive, mirror it to an internal registry.
- **Composability** — nothing stops you from putting a skill alongside the container image and Helm chart for the same service, all in one component version.

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
```

Repository reference formats:
- Local CTF: `./my-catalogue`
- OCI registry: `ghcr.io/myorg`
- Explicit type: `CTF::./my-catalogue` or `OCI::ghcr.io/myorg`
