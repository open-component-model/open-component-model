---
title: "Migrate from Sources to Resources"
icon: "arrow-autofit-right"
slug: "migrate-from-sources"
description: "Replace deprecated component sources and srcRefs with resources linked by the ocm.software/artifact-references label."
weight: 50
toc: true
---

## Goal

Replace the deprecated `sources` of a component and the `srcRefs` that point at them with resources, linked by the
standardized `ocm.software/artifact-references` label.

{{< callout context="caution" >}}
Component `sources` are deprecated. They still work, but `ocm add cv` prints a warning for every component version
that declares them. `ocm transfer cv` copies sources that refer to external content by reference only and logs that
they cannot be transferred. A component version with sources stored as local blobs cannot be transferred at all.
{{< /callout >}}

## Why Migrate?

A source carries no digest. Neither the component version digest nor a signature covers its content, so whoever
controls the source location can replace that content without anyone noticing. Transfer cannot copy an external
source either: without a digest there is nothing to verify the copy against, so only the reference travels and the
target keeps depending on the original location. A source stored as a local blob is not copied either, so transfer
refuses component versions that have one.

A resource gets a digest when the component version is built. The digest is part of the component version digest and
of every signature, and transfer can copy the content and check it. The `ocm.software/artifact-references` label,
defined by the [OCM specification](https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/06-conventions.md#artifact-linking-label),
expresses the same "built from" link that `srcRefs` expressed, and as a label it is signed as well.

## Prerequisites

- [OCM CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) installed
- A component constructor that declares `sources`, and possibly `srcRefs` on its resources

## Steps

| Before | After |
| --- | --- |
| An entry under `sources:` | The same entry under `resources:`, with a resource `type` and a `relation` |
| `srcRefs` on a resource, selecting a source | An `ocm.software/artifact-references` label on that resource, selecting the former source by identity |

### 1. Move each source to the resources

Move every entry from `sources:` to `resources:` and keep its name and version. A source with an `access` usually
refers to content outside the component version, so give it `relation: external`. A source with an `input` is stored
in the component version, which is the default `relation: local`.

`ocm add cv` now computes a digest for the moved entry. For a Git access this pins `ref` to a commit; you no longer
have to set `commit` yourself, although you still may.

{{< callout context="note" >}}
The resource name must not collide with an existing resource of the same version and extra identity. Rename the former
source, for example with a `-source` suffix, if it does.
{{< /callout >}}

### 2. Replace `srcRefs` with the label

The label goes on the derived artifact, the resource that was built from the source, and its `identity` selects the
former source. Each `srcRefs` entry becomes one `identity` in the label value: `identitySelector.name` becomes
`identity.name`, and the remaining `identitySelector` keys become extra identity keys, which must match the extra
identity of the former source exactly. `version` is optional; without it any version matches.

{{< tabs "migrate-from-sources" >}}
{{< tab "Before" >}}

```yaml
components:
  - name: github.com/acme.org/myapp
    version: 1.0.0
    provider:
      name: acme.org
    resources:
      - name: myapp-image
        type: ociImage
        version: 1.0.0
        relation: external
        access:
          type: OCIImage/v1
          imageReference: ghcr.io/acme/myapp:1.0.0
        srcRefs:
          - identitySelector:
              name: myapp-source
    sources:
      - name: myapp-source
        type: git
        version: 1.0.0
        access:
          type: Git/v1
          repository: https://github.com/acme/myapp.git
          ref: refs/tags/v1.0.0
          commit: 7fd1a60b01f91b314f59955a4e4d4e80d8edf11d
```

{{< /tab >}}
{{< tab "After" >}}

```yaml
components:
  - name: github.com/acme.org/myapp
    version: 1.0.0
    provider:
      name: acme.org
    resources:
      - name: myapp-image
        type: ociImage
        version: 1.0.0
        relation: external
        access:
          type: OCIImage/v1
          imageReference: ghcr.io/acme/myapp:1.0.0
        labels:
          - name: ocm.software/artifact-references
            version: v1alpha1
            signing: true
            value:
              - identity:
                  name: myapp-source
      - name: myapp-source
        type: git
        version: 1.0.0
        relation: external
        access:
          type: Git/v1
          repository: https://github.com/acme/myapp.git
          ref: refs/tags/v1.0.0
```

{{< /tab >}}
{{< /tabs >}}

Set `signing: true` on the label so that signatures cover the link as well as the content.

### 3. Build and check the component version

```bash
ocm add cv
ocm get cv ./transport-archive//github.com/acme.org/myapp:1.0.0 -o yaml
```

`ocm add cv` no longer prints the sources warning. In the output, `component.sources` is empty, and the former
source is listed under `resources` with a `digest`.

## Consumers

Tools that followed `srcRefs` must look up the label instead: iterate over the resources of the component version and
select those whose `ocm.software/artifact-references` label (version `v1alpha1`) has an `identity` matching the
subject, as described in the
[lookup algorithm](https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/06-conventions.md#lookup-algorithm).
To find what a resource was built from, read its label and resolve each `identity` against the resources of the same
component version.

Component versions that were published with sources keep them: changing a published component version changes its
digest and breaks existing signatures. Migrate when you build the next version.
