---
title: "Component Identity"
description: "How OCM identifies components, versions, and artifacts — from naming to descriptors."
icon: "🪪"
weight: 1
url: /docs/concepts/component-identity
toc: true
---

## Overview

OCM uses a coordinate system to uniquely identify every piece of software — from entire components down to individual artifacts. This page explains how that system works, what a component descriptor contains, and how identity stays stable regardless of where artifacts are stored.

For the high-level introduction, see [The OCM Core Model]({{< relref "docs/overview/core-model.md" >}}).

## Components and Component Versions

A **component** is a logical unit of software — a frontend, a database, an autoscaler, or an entire application. Components group a set of semantically related **component versions**.

Each component version is an immutable snapshot that contains:

- **Resources**: the deliverables (OCI images, Helm charts, binaries, configuration files).
- **Sources**: the inputs from which resources were built (git repositories, source archives).
- **References**: dependencies on other component versions.

A component version is always described by a [component descriptor](#the-component-descriptor) — a YAML document that lists every element and how to access it.

## Component Identity

Every component is identified by a **name** and a **version**. Together, they form the component's globally unique identity.

### Component Name

Component names follow a DNS-based naming scheme:

```text
<DNS domain>/<name component>{/<name component>}
```

The DNS domain at the start of the name (e.g., `acme.org` in `acme.org/webshop/backend`) must be controlled by the component owner, which prevents naming collisions across organizations. The remaining path segments further classify the component within the owner's namespace.

**Examples:**

| Component Name | Owner |
|---|---|
| `github.com/open-component-model/ocm` | OCM project |
| `acme.org/webshop/backend` | Acme Corp |
| `acme.org/webshop/frontend` | Acme Corp |

The name must match the regex: `^[a-z][-a-z0-9]*([.][a-z][-a-z0-9]*)*[.][a-z]{2,}(/[a-z][-a-z0-9_]*([.][a-z][-a-z0-9_]*)*)+$`

### Component Version

Versions must adhere to **relaxed SemVer**: major and minor segments are required, patch is optional, and a `v` prefix is allowed.

**Valid versions:** `1.0.0`, `v2.1`, `1.0.0-rc.1`, `v3.2.1+build.42`

### Combined Identity

A component identity is written as `name:version`:

```text
github.com/acme/webshop:1.0.0
```

This uniquely identifies one specific immutable snapshot of the `webshop` component.

## Artifact Identity

Artifact identities are always **relative to a component version**. They are not globally unique on their own — they only become globally unique when combined with a component identity.

Within a component version, every artifact (resource, source, or reference) must have a unique identity, defined by the following attributes:

| Attribute | Required | Purpose |
|---|---|---|
| `name` | Yes | Identifies the artifact. Typically describes its intended purpose (e.g., `backend-image`, `deploy-chart`). |
| `extraIdentity` | No | A string-to-string map that contributes to identity. Used for variants like `os: linux`, `architecture: amd64`. |
| `version` | No | Only contributes to identity when `name` + `extraIdentity` are not sufficient to disambiguate. |

**Global artifact identity** = component name + component version + artifact identity.

For example, the resource `backend-image` in `github.com/acme/webshop:1.0.0` has the global identity:

```text
github.com/acme/webshop:1.0.0:resource/backend-image
```

### Extra Identity for Variants

When a component version contains multiple variants of the same artifact (e.g., multi-architecture images), use `extraIdentity` to distinguish them:

```yaml
resources:
  - name: backend-image
    type: ociImage
    extraIdentity:
      architecture: amd64
      os: linux
  - name: backend-image
    type: ociImage
    extraIdentity:
      architecture: arm64
      os: linux
```

Both resources share the name `backend-image`, but their `extraIdentity` attribute makes them unique within the component version.

## Coordinate Notation

OCM coordinates provide a shorthand for referencing elements in the model. Given a component `example.org/my-component` with versions `1.2.3` and `1.3.0`, each declaring a resource `my-resource`:

| Coordinate | Refers to |
|---|---|
| `example.org/my-component` | All versions of the component (1.2.3 + 1.3.0) |
| `example.org/my-component:1.2.3` | Version 1.2.3 of the component |
| `example.org/my-component:1.2.3:resource/my-resource` | The resource `my-resource` in version 1.2.3 |

The general pattern is:

```text
<component-name>[:<version>[:<artifact-type>/<artifact-name>]]
```

## The Component Descriptor

The component descriptor is a YAML document that fully describes a component version. It is the central data structure in OCM. It lists three kinds of elements — **resources** (deliverables like images and charts), **sources** (inputs like git repos), and **references** (dependencies on other component versions). Each element carries identity fields (`name`, optional `extraIdentity`), a type, and an access specification. Labels can be attached to the component or any element as extensible key-value metadata.

For the full structure and field reference, see [Component Descriptor Reference]({{< relref "docs/reference/component-descriptor.md" >}}).

## Artifact Types

Artifact types describe the semantic meaning of a resource or source. OCM defines two categories:

- **Central types** use camelCase and are part of the OCM specification (e.g., `ociImage`, `helmChart`, `blob`).
- **Vendor-specific types** use DNS-based naming to avoid collisions (e.g., `landscaper.gardener.cloud/blueprint`).

The artifact type determines how tools interpret the content. It is independent of how the artifact is stored or accessed.

For more details, see [Artifact Types](https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/01-artifact-types/README.md) in the specification.

## Access Specifications

Access specifications decouple an artifact's identity from its storage location. The `access.type` field determines how to retrieve the artifact, while the remaining fields provide the location-specific details.

The same artifact type can be accessed through different access specifications. For example, an `ociImage` could be stored in an OCI registry (`ociArtifact`) or bundled into a CTF archive (`localBlob`).

For available access types and their fields, see [Access Specification]({{< relref "docs/reference/component-descriptor.md" >}}#access-specification) and [Input and Access Types]({{< relref "docs/reference/input-and-access-types.md" >}}).

## Component Repositories and Storage

Component versions are stored in **component repositories**. OCM is technology-agnostic — the same component version can be stored in different repository implementations:

- **OCI registries** (e.g., GitHub Container Registry, Harbor, Docker Hub)
- **CTF (Common Transport Format) archives** — OCI-layout-compatible archives for air-gapped transfers. See [Create Component Versions]({{< relref "docs/getting-started/create-component-version.md" >}}) for details.

## Signing and Verification

Component versions can be cryptographically signed to ensure integrity and provenance. The signing model has important properties:

- **Digests** cover resources and references, providing tamper detection.
- **Access specifications are excluded** from the digest because they change when artifacts are transported to a new location — this is what makes location independence possible.
- **Multiple signatures** are supported, allowing different parties to independently attest to a component version.

For hands-on signing instructions, see [Sign Component Versions]({{< relref "docs/how-to/sign-component-version.md" >}}).

## Next Steps

- [Create Component Versions]({{< relref "docs/getting-started/create-component-version.md" >}}) — build component versions with the OCM CLI.

## Related Documentation

- [The OCM Core Model]({{< relref "docs/overview/core-model.md" >}}) — high-level introduction to OCM's building blocks.
- [Component Descriptor Reference]({{< relref "docs/reference/component-descriptor.md" >}}) — field-by-field reference for the component descriptor.
- [OCM Specification](https://github.com/open-component-model/ocm-spec) — the formal specification of the Open Component Model.
