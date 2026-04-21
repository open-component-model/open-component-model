---
title: "Model Software Products"
description: "Compose multi-component products using component references, versioning, and aggregation."
icon: "🗂️"
weight: 48
toc: true
---

## Goal

Most products consist of multiple independently versioned services.
This guide uses OCM component references to pin those services into a single [Software Bill of Delivery](https://ocm.software/docs/overview/benefits-of-ocm/#create-a-software-bill-of-delivery) — a versioned, auditable record of what a release contains.

## You'll end up with

- A `component-constructor.yaml` that defines service components and a product-level aggregator
- Component references that pin exact version combinations
- A portable [CTF archive]({{< relref "docs/concepts/transfer-concept.md#common-transport-format-ctf" >}}) ready for transfer across registries and air-gapped environments

**Estimated time:** ~15 minutes

## Prerequisites

- [OCM CLI]({{< relref "ocm-cli-installation.md" >}}) installed
- Familiarity with [the component constructor]({{< relref "../tutorials/advanced-component-constructor.md" >}})

## Steps

{{< steps >}}
{{< step >}}

### Create a component per service

Each service gets its own component. Create a single `component-constructor.yaml` — you will add all components to this one file. Start with the services:

```shell
cat > component-constructor.yaml << 'EOF'
# yaml-language-server: $schema=https://ocm.software/latest/schemas/bindings/go/constructor/schema-2020-12.json
components:

# -- Backend service
- name: ocm.software/how-to/backend
  version: 3.1.0
  provider:
    name: ocm.software
  resources:
    - name: image
      type: ociImage
      version: 3.1.0
      relation: local
      access:
        type: OCIImage/v1
        imageReference: ghcr.io/stefanprodan/podinfo:6.9.0

# -- Frontend service
- name: ocm.software/how-to/frontend
  version: 1.5.0
  provider:
    name: ocm.software
  resources:
    - name: image
      type: ociImage
      version: 1.5.0
      relation: local
      access:
        type: OCIImage/v1
        imageReference: ghcr.io/stefanprodan/podinfo:6.9.1

# -- Cache (Redis)
- name: ocm.software/how-to/cache
  version: 2.0.0
  provider:
    name: ocm.software
  resources:
    - name: image
      type: ociImage
      version: 7.2.4
      relation: external
      access:
        type: OCIImage/v1
        imageReference: docker.io/library/redis:7.2.4
EOF
```

{{< callout title="Note" icon="outline/info-circle" >}}
This guide uses the same demo image (`ghcr.io/stefanprodan/podinfo`) for frontend and backend services.
In a real scenario, each service would reference its own container image.  
**`relation`** — Use `local` when the artifact is maintained by the team providing the component (e.g. your own service image).
Use `external` when it comes from a third party (e.g. a public Redis image you depend on).
{{< /callout >}}

{{< /step >}}
{{< step >}}

### Add a product component with references

Append the following product component to the **same** `component-constructor.yaml`. It aggregates the service components via `componentReferences`, pinning the exact combination of service versions that were validated together:

```shell
cat >> component-constructor.yaml << 'EOF'

# -- Product component (aggregator)
- name: ocm.software/how-to/product
  version: 2.0.0
  provider:
    name: ocm.software
  componentReferences:
    - name: backend
      componentName: ocm.software/how-to/backend
      version: 3.1.0
    - name: frontend
      componentName: ocm.software/how-to/frontend
      version: 1.5.0
    - name: cache
      componentName: ocm.software/how-to/cache
      version: 2.0.0
EOF
```

A product component uses [`componentReferences`]({{< relref "docs/reference/component-constructor.md#component-references" >}}) to declare which service versions belong together in a release. The product component itself carries no resources — it acts purely as a **Software Bill of Delivery** that pins a tested combination of versions. This gives release managers, auditors, and downstream consumers a clear, immutable record of what a release contains.

{{< /step >}}

{{< step >}}

### Create all component versions

Run from the directory containing your `component-constructor.yaml`:

```shell
ocm add cv
```

> **Note:** `component-constructor.yaml` is the default filename the CLI looks for when no `--constructor` flag is given. If your file has a different name or location, pass `--constructor <path>` explicitly.
>
> If the `--repository` flag is not specified, the CLI creates a CTF archive called `transport-archive` in the current directory by default.

<details>
  <summary>Expected output</summary>

```text
 COMPONENT                    │ VERSION │ PROVIDER
──────────────────────────────┼─────────┼──────────
 ocm.software/how-to/product  │ 2.0.0   │ ocm.software
 ocm.software/how-to/backend  │ 3.1.0   │
 ocm.software/how-to/frontend │ 1.5.0   │
 ocm.software/how-to/cache    │ 2.0.0   │
```

> **Note:** In the CLI table output, the `PROVIDER` column is printed only for the first component when all components share the same provider. The value still applies to every row.

</details>

{{< /step >}}
{{< step >}}

### Verify the result

```shell
ocm get cv ./transport-archive//ocm.software/how-to/product:2.0.0 --recursive=-1 -o tree
```

<details>
  <summary>Expected output</summary>

```text
 NESTING  COMPONENT                     VERSION  PROVIDER      IDENTITY
 └─ ●     ocm.software/how-to/product   2.0.0    ocm.software  name=ocm.software/how-to/product,version=2.0.0
    ├─    ocm.software/how-to/backend   3.1.0    ocm.software  name=ocm.software/how-to/backend,version=3.1.0
    ├─    ocm.software/how-to/frontend  1.5.0    ocm.software  name=ocm.software/how-to/frontend,version=1.5.0
    └─    ocm.software/how-to/cache     2.0.0    ocm.software  name=ocm.software/how-to/cache,version=2.0.0
```
</details>

{{< /step >}}

{{< step >}}

### Create a New Version

Say the backend team releases a patch (`3.1.0` → `3.2.0`). Replace the constructor with the new backend version, bump the product aggregator to `2.1.0`, and keep the remaining components unchanged:

```shell
cat > component-constructor.yaml << 'EOF'
# yaml-language-server: $schema=https://ocm.software/latest/schemas/bindings/go/constructor/schema-2020-12.json
components:

# -- Backend service (bumped)
- name: ocm.software/how-to/backend
  version: 3.2.0
  provider:
    name: ocm.software
  resources:
    - name: image
      type: ociImage
      version: 3.2.0
      relation: external
      access:
        type: OCIImage/v1
        imageReference: ghcr.io/stefanprodan/podinfo:6.9.1

# -- Frontend service (unchanged)
- name: ocm.software/how-to/frontend
  version: 1.5.0
  provider:
    name: ocm.software
  resources:
    - name: image
      type: ociImage
      version: 1.5.0
      relation: local
      access:
        type: OCIImage/v1
        imageReference: ghcr.io/stefanprodan/podinfo:6.9.1

# -- Cache (unchanged)
- name: ocm.software/how-to/cache
  version: 2.0.0
  provider:
    name: ocm.software
  resources:
    - name: image
      type: ociImage
      version: 7.2.4
      relation: external
      access:
        type: OCIImage/v1
        imageReference: docker.io/library/redis:7.2.4

# -- Product component (bumped)
- name: ocm.software/how-to/product
  version: 2.1.0
  provider:
    name: ocm.software
  componentReferences:
    - name: backend
      componentName: ocm.software/how-to/backend
      version: 3.2.0  # updated
    - name: frontend
      componentName: ocm.software/how-to/frontend
      version: 1.5.0  # unchanged
    - name: cache
      componentName: ocm.software/how-to/cache
      version: 2.0.0  # unchanged
EOF
```

{{< /step >}}
{{< step >}}

### Recreate

```shell
ocm add cv --repository transport-archive-updated
```

> **Note:** This creates a new CTF in the `transport-archive-updated` folder, keeping the original `transport-archive` intact for comparison.

<details>
  <summary>Expected output</summary>

```text
 COMPONENT                    │ VERSION │ PROVIDER
──────────────────────────────┼─────────┼──────────
 ocm.software/how-to/product  │ 2.1.0   │ ocm.software
 ocm.software/how-to/backend  │ 3.2.0   │
 ocm.software/how-to/frontend │ 1.5.0   │
 ocm.software/how-to/cache    │ 2.0.0   │
```
</details>

{{< /step >}}
{{< /steps >}}

## Cleanup

Remove everything created in this guide:

```shell
rm -rf transport-archive transport-archive-updated component-constructor.yaml
```

## Next Steps

- [Create a Multi-Component Product]({{< relref "docs/tutorials/advanced-component-constructor.md" >}}) — advanced constructor patterns with nesting, labels, and sources

## Related Documentation

- [Component Descriptor Reference]({{< relref "docs/reference/component-descriptor.md" >}}) — understand the structure of the generated release metadata
- [OCM Specification: References](https://github.com/open-component-model/ocm-spec/blob/main/doc/02-processing/01-references.md) — how component references are resolved during transport and deployment
