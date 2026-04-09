---
title: "The OCM Core Model"
description: "Understand the fundamental building blocks of the Open Component Model."
icon: "🧩"
weight: 13
toc: true
---

## The Problem: Software Delivery is Fragmented

Modern software is assembled from many different artifacts — container images, Helm charts, configuration files, binaries — stored across many different registries and repositories. There is no standard way to describe, version, sign, or transport a complete delivery as a single unit.

This fragmentation leads to real problems:

- **Broken deployments** when artifacts are missing, mismatched, or out of date.
- **Unclear provenance** when you cannot trace which source code produced which binary.
- **Compliance gaps** when auditors cannot verify what was delivered and where it came from.

OCM solves this by introducing a technology-agnostic model for describing software deliveries.

## Components: A Universal Envelope

In OCM, a **component** groups everything needed for a delivery into one logical unit. Think of it as a universal envelope that holds all the pieces of your software together.

Each component has:

- A **name** based on DNS naming (e.g., `github.com/acme/webshop`) — globally unique and controlled by the domain owner.
- A **version** following relaxed [SemVer](https://semver.org/) (e.g., `1.0.0` or `v2.1`) — each version is an immutable snapshot. See [Component Identity]({{< relref "docs/concepts/component-identity.md" >}}) for the exact version format rules.

Together, `github.com/acme/webshop:1.0.0` uniquely identifies a specific delivery of the webshop.

## What's Inside a Component Version

A component version contains three kinds of elements:

| Element       | Purpose                                           | Examples                          |
|---------------|---------------------------------------------------|-----------------------------------|
| **Resources** | The deliverables — what gets deployed              | OCI images, Helm charts, binaries |
| **Sources**   | Where resources were built from                    | Git repositories, source archives |
| **References**| Dependencies on other component versions           | Shared libraries, base components |

All of these are described in a single **Component Descriptor** — a YAML document that serves as the manifest for the component version. For the full structure and field reference, see [Component Descriptor Reference]({{< relref "docs/reference/component-descriptor.md" >}}).

## Identity and Coordinates

OCM uses a coordinate system to uniquely identify every piece of software:

- **Component identity** = name + version → globally unique across the ecosystem.
- **Artifact identity** = name (+ optional extra identity attributes) → unique within a component version.
- **Coordinate notation** combines both: `github.com/acme/webshop:1.0.0:resource/backend-image`.

For a deep dive into how identity works, see [Component Identity]({{< relref "docs/concepts/component-identity.md" >}}).

## Location Independence

A key design principle of OCM is that **identity is separate from storage location**. The same component version can live in:

- OCI registries (e.g., GitHub Container Registry, Docker Hub)
- S3-compatible object storage
- Local filesystems
- CTF (Common Transport Format) archives

This means you can transport component versions across boundaries — from a public registry to an air-gapped environment, or between cloud providers — without changing their identity or breaking their signatures.

## Related Documentation

- [Component Identity]({{< relref "docs/concepts/component-identity.md" >}}) — deep dive into how OCM identifies components, versions, and artifacts.
- [Create Component Versions]({{< relref "docs/getting-started/create-component-version.md" >}}) — build your first component version with the OCM CLI.
