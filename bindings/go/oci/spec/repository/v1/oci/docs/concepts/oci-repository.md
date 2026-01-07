# OCI Repository

An **OCI repository** in OCM is a structured representation of a repository namespace hosted by an OCI-compatible registry, as defined by the OCI Distribution Specification.

---

## Purpose

OCM models an OCI repository explicitly to avoid ambiguity between:

- **Registry endpoint** (host, scheme, port)
- **Repository namespace** (path within the registry)

This enables consistent addressing, validation, and tooling behavior.

---

## Structure

An OCM OCI repository is defined by two parts:

- **BaseURL**  
  The registry endpoint (e.g. `ghcr.io`, `https://registry.example.com:5000`)

- **SubPath**  
  The repository namespace within the registry (e.g. `open-component-model/ocm`)

Together, they uniquely identify the repository location.

---

## Why This Matters

Explicit repository modeling allows OCM to:

- Normalize user input (embedded paths vs. explicit subpaths)
- Support multiple repositories per registry
- Reason about transfers, mirroring, and relocation
- Avoid implicit parsing rules in higher-level tooling

---

## Usage

OCI repositories are used whenever OCM:

- Pushes or pulls component versions
- Resolves component references
- Transfers components between registries

They are **configuration objects**, not artifacts.

---

## Summary

An OCM OCI repository:

- Represents an OCI repository namespace explicitly
- Separates registry endpoint from repository path
- Provides a stable abstraction for OCM tooling
- Remains fully compatible with OCI semantics