---
title: Component Descriptor
description: "Reference documentation for the OCM Component Descriptor format (v2, JSON Schema 2020-12)."
weight: 2
toc: false
aliases:
  - /docs/reference/component-descriptor/
---

The **OCM Component Descriptor** is the central data structure of the Open Component Model.
It describes a component version with its resources, sources, references, labels, and signatures.

{{< callout context="caution" title="Sources are deprecated" icon="outline/alert-triangle" >}}
The `component.sources` field is deprecated. Sources carry no digest, so neither the component version digest nor a
signature covers their content. Declare this content as resources instead; see
[Migrate from Sources to Resources]({{< relref "docs/changelog/migrate-from-sources.md" >}}).
{{< /callout >}}

The schema below defines the full structure of a component descriptor as specified by
[JSON Schema 2020-12](https://json-schema.org/draft/2020-12/schema).

---

{{< schema-renderer url="/schemas/bindings/go/descriptor/v2/schema-2020-12.json" >}}
