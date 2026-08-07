---
title : "OCM Coordinates"
description: "Discover how OCM components and artifacts are identified."
icon: "🧭"
weight: 32
toc: true
---

*OCM coordinates* are used to reference OCM component versions and the artifacts within OCM component versions. Coordinates referring to an OCM component version are also called *component identity*, whereas relative coordinates referring to an artifact are called *artifact identity*.

## Component Identity

Component identities are globally unique and may be used to refer to full component versions. They are defined through the following attributes:

- `name`: Identifies a component. Must start with a URL prefix that should be controlled by the owner of the component to avoid collisions.
- `version`: If used with a component name, identifies a specific component version. Must adhere to "relaxed SemVer" (major, minor (+ optional patch level) - optional v-prefix).

## Artifact Identity

Artifact identities are always relative to a component version and may only be used in conjunction with a component identity. Within a component version, all artifacts must have a unique identity.

Artifact identities are defined through the following attributes:

- `name`: Identifies an artifact. Typically expresses the intended purpose.
- `extraIdentity` (optional): Contributes to the identity of the artifact. String-to-string map.

## Examples

Let's assume there is a component named `example.org/my-component` with two versions, `1.2.3` and `1.3.0`, declaring a resource with the name `my-resource`. The following OCM coordinates can be used to reference different elements:

- `example.org/my-component`: all versions of the component (1.2.3 + 1.3.0)
- `example.org/my-component:1.2.3`: version 1.2.3 of the component
- `example.org/my-component:1.2.3:resource/my-resource`: `my-resource` as declared by the component version
