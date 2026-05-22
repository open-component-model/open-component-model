---
title: "Shipping Coding Agent Skills with OCM"
date: 2026-05-22T10:00:00+02:00
draft: false
description: "A vendor-neutral, airgap-capable approach to versioning, signing, and distributing coding agent skills using OCI registries."
summary: "Coding agents accumulate useful configuration fast, but there's no standard way to share it across machines, teams, or environments. This post walks through using OCM to package and distribute agent skills over any OCI registry."
categories: ["cli"]
tags: ["skills", "agent", "oci", "supply-chain"]
contributors: []
---

Most teams using coding agents — Claude Code, Codex CLI, Cursor, Continue, or anything else — end up with a folder of Markdown files that make the agent useful for their specific stack. How to handle errors in their Go codebase, how their database migrations are structured, what the deploy pipeline expects. These accumulate over time and become genuinely valuable.

Then someone joins the team and has none of them. Or you get a new laptop. Or you want the same context available in CI.

At that point the options are: manually copy files, check them into a dotfiles repo with no versioning story, or describe them in a README. None of that scales well, and none of it gives you any way to verify where the files came from.

## Where things live today

The paths differ by agent but the pattern is the same:

- Claude Code: `~/.claude/skills/<name>/SKILL.md`
- OpenAI Codex CLI: `~/.agents/skills/<name>/SKILL.md`
- Cursor: `.cursorrules`
- Continue: `.continue/`

Each of these is just a file on disk. The agents pick them up on startup. There's nothing wrong with the format — the problem is that file-on-disk is where the distribution story ends.

## Using OCM as the transport layer

The [Open Component Model](https://ocm.software) treats software artifacts as versioned, signed bundles stored in OCI registries. It was built for container images and Helm charts, but the abstraction is general: a component version is a named bundle of resources, and a resource is any blob with a type label.

That fits skills well. A skill gets packaged as a resource with type `ai.skill/v1` inside a catalogue component:

```text
Component: myorg.io/skill-catalogue   version: 1.0.0
├── resource: ocm-guide        type: ai.skill/v1
├── resource: golang-patterns   type: ai.skill/v1
└── resource: backend-patterns  type: ai.skill/v1
```

`ai.skill/v1` is just a string — OCM stores it as a blob without needing any registry-side support. The registry you already use for container images works as-is.

What OCM adds on top:

- **Signing**: component versions can be signed with cosign or GPG, so you can verify a skill catalogue came from your organisation and not some random source.
- **Air-gap support**: OCM's CTF (Common Transport Format) is a portable local archive. You can ship a catalogue on a USB drive, mirror it to an internal registry, or include it in an offline software delivery bundle.
- **Composability**: nothing stops you from putting skills in the same component version as the container image and Helm chart for a service. The agent context travels with the software it supports.

## What a skill file looks like

A skill is a Markdown file with a YAML frontmatter block. The agent reads `name` and `description` to decide when to activate it:

```markdown
---
name: golang-patterns
description: Idiomatic Go patterns for this codebase. Use when writing or reviewing Go.
tools: ["Read", "Bash"]
---

# Go Patterns

## Error handling
Wrap errors at the call site with context...
```

OCM doesn't care about the content — it just stores and retrieves bytes.

## Packaging skills

Point `ocm skill push` at a directory of skill subdirectories:

```bash
ocm skill push ./my-skills \
  --component myorg.io/skill-catalogue \
  --version 1.0.0 \
  --output constructor.yaml
```

This writes a `constructor.yaml` with each `SKILL.md` as a file input. Then package it:

```bash
ocm add component-version \
  --repository ./my-catalogue \
  --constructor constructor.yaml
```

That produces a local CTF archive. No registry needed to get started.

## Installing skills

Pull a specific skill into Claude Code:

```bash
ocm skill pull ./my-catalogue//myorg.io/skill-catalogue:1.0.0 --skill golang-patterns
```

Or install into Codex CLI instead:

```bash
ocm skill pull ./my-catalogue//myorg.io/skill-catalogue:1.0.0 \
  --skill golang-patterns --target codex
```

Or both at once:

```bash
ocm skill pull ./my-catalogue//myorg.io/skill-catalogue:1.0.0 --target all
```

Omit `--skill` to pull everything in the catalogue.

## Sharing with a team

Swap the local archive for a registry URL:

```bash
ocm add component-version \
  --repository ghcr.io/myorg \
  --constructor constructor.yaml

ocm skill pull ghcr.io/myorg//myorg.io/skill-catalogue:1.0.0
```

Standard OCI credential helpers handle authentication — `docker login ghcr.io` is enough. For air-gapped environments, `ocm transfer` mirrors the CTF archive to an internal registry.

## Why this matters beyond the tooling

Most teams treating agent configuration as informal dotfiles will eventually want to version it properly, audit where it comes from, or ship it into environments without internet access. OCM already solves all three for container images and Helm charts. Plugging agent skills into the same mechanism means you get those properties without building anything new.

The `--target` flag keeps things portable across agents. The same catalogue works for whatever tools your team is using today and whatever replaces them later.

## Full command reference

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
