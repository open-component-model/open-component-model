---
title: "Component Name Grammar Aligned with OCI"
icon: "arrow-autofit-right"
slug: "component-name-grammar-oci"
description: "The component-name grammar now follows the OCI repository path grammar: digit-led and digit-only segments are accepted, and OCI-invalid names are rejected."
weight: 80
toc: true
---

## Goal

Align the component-name grammar with the one real constraint, the OCI repository path grammar, so a name maps directly to the OCI repository path `<subPath>/component-descriptors/<name>`. This is grammar compatibility. The name itself is limited to 255 characters, but OCI distribution registries cap the repository path after the registry host at 255 characters, so the `component-descriptors/` prefix (22 characters) and any `subPath` reduce the usable name length. A name that is valid on its own may still be rejected by a registry when the full path is too long; that limit is enforced at the storage backend.

## What Changed

The `componentName` pattern in the descriptor v2 and constructor v1 schemas, and `compref.ComponentRegex`, moved from:

```text
^[a-z][-a-z0-9]*([.][a-z][-a-z0-9]*)*[.][a-z]{2,}(/[a-z][-a-z0-9_]*([.][a-z][-a-z0-9_]*)*)+$
```

to:

```text
^[a-z0-9]([-a-z0-9]*[a-z0-9])?([.][a-z0-9]([-a-z0-9]*[a-z0-9])?)*[.][a-z]{2,}(/[a-z0-9]+(([.]|_|__|-+)[a-z0-9]+)*)+$
```

- **Relaxed:** the letter-first rule is dropped. DNS labels and path segments may start with a digit, and path segments may consist only of digits (`1password.com/cli`, `acme.org/1`, `acme.org/1foo`, `acme.org/v1/2`).
- **Tightened:** names that are invalid in OCI are rejected at validation time instead of failing later at the store (`acme.org/foo-`, `acme.org/foo_`, `acme.org/a_-b`, `acme.org/a___b`, `acme-.org/x`).
- **Reserved:** the `component-descriptors` segment is reserved for the storage mapping and is rejected in a name.

Lowercase-only is unchanged. Uppercase would need a new name mapping and is out of scope.

## Compatibility with Legacy OCM (v1)

- **Tightening OCI-invalid names is compatible under the `urlPath` mapping:** `urlPath` is the only mapping the new tooling implements, and under it the names rejected here (`acme.org/foo-`, `acme.org/a___b`, `acme-.org/x`, ...) could never be stored, so no working name is lost. Legacy OCM (v1) also offered a deprecated `sha256-digest` mapping that hashes the name into the repository path and could store such names; a component published only that way is no longer accepted by the new grammar, so audit for these names and rename them before you migrate.
- **Reserving `component-descriptors` is a narrow exception:** a name such as `acme.org/component-descriptors` is a valid OCI path and passed the old grammar, but it collides with the storage prefix and is now rejected. Referencing such a name was already unreliable, because the reference parser splits on the last `component-descriptors` segment. If you published one, rename the component before you upgrade.
- **Dropping the letter-first rule is not compatible:** legacy OCM (v1) rejects digit-led names such as `acme.org/1`. Digit-led and digit-only names are readable only by legacy tooling from the patch release that relaxes the letter-first rule in its embedded schemas.

{{< callout context="note" >}}
Minimum legacy OCM (v1) version that reads digit-led component names: to be filled in once the legacy patch release is cut (tracked in [open-component-model/ocm#2186](https://github.com/open-component-model/ocm/pull/2186)).
{{< /callout >}}

## Action Required

If you used the default `urlPath` mapping, names that already work keep working, except a name containing a `component-descriptors` segment (see the reserved-segment note above), which is now rejected and must be renamed. If you used the deprecated `sha256-digest` mapping, audit for names the new grammar rejects (trailing or doubled separators, uppercase, or a `component-descriptors` segment) and rename them before migrating. If you adopt a digit-led or digit-only name, make sure every consumer still on legacy OCM (v1) is at least the minimum version above before you publish it.
