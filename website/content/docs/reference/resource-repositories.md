---
title: "Resource Repositories"
description: "Technical reference for built-in resource repositories: supported access types, credential resolution, and capabilities."
weight: 6
toc: true
---

This page is the technical reference for built-in resource repositories. For an introduction to what resource
repositories are and why they exist, see
[Concept: Resource Repositories]({{< relref "docs/concepts/resource-repositories.md" >}}).

---

## OCI Resource Repository

Handles OCI artifacts stored in OCI-compliant registries.

### Supported Access Types

| Access Type                                                            | Aliases                                  |
|------------------------------------------------------------------------|------------------------------------------|
| [`OCIImage/v1`]({{< relref "input-and-access-types.md" >}}#ociimagev1) | `ociArtifact`, `ociRegistry`, `ociImage` |

### Capabilities

| Operation         | Supported |
|-------------------|-----------|
| Download          | Yes       |
| Upload            | Yes       |
| Digest Processing | Yes       |

### Credential Resolution

The credential consumer identity is derived from the `imageReference` field in the access specification. The registry
hostname is extracted from the image reference to construct an identity of type `OCIRegistry`.

**Example:** For a resource with access `imageReference: ghcr.io/acme/myapp:1.0.0`, the resolved identity is:

| Attribute  | Value         |
|------------|---------------|
| `type`     | `OCIRegistry` |
| `hostname` | `ghcr.io`     |
| `scheme`   | `https`       |

This identity is then matched against configured consumers in the credential system.
See [Credential Consumer Identities: OCIRegistry]({{< relref "credential-consumer-identities.md" >}}#ociregistry) for
matching rules.

### Download Behavior

Downloads the complete OCI artifact (manifest and layers) from the registry. The returned blob represents the artifact
in its OCI format.

### Upload Behavior

Pushes an OCI artifact to the target registry. The resource descriptor is updated with the repository-specific access
information (e.g., the final image reference with digest) after upload.

### Digest Processing

The OCI resource repository also implements digest processing. After a resource is stored, OCM can query the registry to
retrieve and verify the artifact's digest, ensuring the access specification is pinned to an immutable reference.

---

## Helm Resource Repository

Handles Helm charts stored in HTTP/HTTPS-based chart repositories.

### Supported Access Types

| Access Type                                                    | Aliases |
|----------------------------------------------------------------|---------|
| [`Helm/v1`]({{< relref "input-and-access-types.md" >}}#helmv1) | `helm`  |

### Capabilities

| Operation         | Supported |
|-------------------|-----------|
| Download          | Yes       |
| Upload            | No        |
| Digest Processing | No        |

{{< callout type="info" >}}
Upload is not supported because traditional Helm chart repositories are read-only HTTP servers that serve a static
`index.yaml` and packaged chart archives. There is no standardized upload API.
{{< /callout >}}

{{< callout type="note" >}}
For Helm charts stored in OCI registries, use the [OCI resource repository](#oci-resource-repository) with an [
`OCIImage/v1`]({{< relref "input-and-access-types.md" >}}#ociimagev1) access type instead.
{{< /callout >}}

### Credential Resolution

The credential consumer identity is derived from the `helmRepository` field in the access specification. The identity
type is `HelmChartRepository`.

**Example:** For a resource with `helmRepository: https://stefanprodan.github.io/podinfo`:

| Attribute  | Value                    |
|------------|--------------------------|
| `type`     | `HelmChartRepository`    |
| `hostname` | `stefanprodan.github.io` |
| `scheme`   | `https`                  |

If the resource has no `helmRepository` (a local chart embedded via input), no credential identity is returned — local
charts do not require remote authentication.

See
[Credential Consumer Identities: HelmChartRepository]
({{< relref "credential-consumer-identities.md" >}}#helmchartrepository)
for matching rules.

### Download Behavior

Downloads the Helm chart (and optional `.prov` provenance file) from the remote repository. The chart is packaged into a
tar archive and returned as an in-memory blob.

The `helmChart` and `helmRepository` fields from the access specification are combined to construct the full chart
reference used for download.

---

## External Resource Repositories (Plugins)

External plugins declare supported access types in their capability specification and implement the same three
operations (resolve credential identity, download, upload) over the plugin protocol. Once installed, OCM routes requests
for matching access types to the plugin automatically.

See [Concept: Plugin System]({{< relref "docs/concepts/plugin-system.md" >}}) for details on building and installing
plugins.

## Related Documentation

- [Concept: Resource Repositories]({{< relref "docs/concepts/resource-repositories.md" >}}): why resource repositories
  exist and how they fit into OCM
- [Reference: Input and Access Types]({{< relref "input-and-access-types.md" >}}): access type specifications handled by
  resource repositories
- [Reference: Credential Consumer Identities]({{< relref "credential-consumer-identities.md" >}}): identity types and
  matching rules for credential resolution
- [Concept: Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}}): how resource repositories
  enable artifact transfer
