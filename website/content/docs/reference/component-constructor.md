---
title: Component Constructor
description: "Reference documentation for the OCM Component Constructor format (v1, JSON Schema 2020-12)."
weight: 1
toc: false
---

The **OCM Component Constructor** defines the input format for building component versions.
It describes how resources and sources are provided -- either via access specifications
(referencing existing artifacts) or via input specifications (providing content directly).

The constructor schema accepts two formats:

- **Variant 1** -- A wrapper object with a `components` list, allowing multiple components
  to be defined in a single file.
- **Variant 2** -- A single component defined directly at the top level with `name`,
  `version`, `provider`, and its associated `resources`, `sources`, and `componentReferences`.

The schema below defines the full structure as specified by
[JSON Schema 2020-12](https://json-schema.org/draft/2020-12/schema).

<a id="component-references"></a>

---

{{< schema-renderer url="/schemas/bindings/go/constructor/schema-2020-12.json" >}}
