---
title: "Component Name Grammar Aligned with OCI"
icon: "arrow-autofit-right"
slug: "component-name-grammar-oci"
description: "The component-name grammar now follows the OCI repository path grammar: digit-led and digit-only segments are accepted, and OCI-invalid names are rejected early."
weight: 80
toc: true
---

## Goal

Align the component-name grammar with the one real constraint, the OCI repository path grammar, so a name is always storable verbatim under `<base>/component-descriptors/<name>`.

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

- **Tightening is compatible:** the OCI-invalid names rejected here could never be stored, so no working name is lost.
- **Dropping the letter-first rule is not:** legacy OCM (v1) rejects digit-led names such as `acme.org/1`. Digit-led and digit-only names are readable only by legacy tooling from the patch release that relaxes the letter-first rule in its embedded schemas.

{{< callout context="note" >}}
Minimum legacy OCM (v1) version that reads digit-led component names: to be filled in once the legacy patch release is cut (tracked in [open-component-model/ocm#2186](https://github.com/open-component-model/ocm/pull/2186)).
{{< /callout >}}

## Action Required

None for names that already work. If you adopt a digit-led or digit-only component name, make sure every consumer that still runs legacy OCM (v1) is on at least the minimum version above before you publish such a component.
