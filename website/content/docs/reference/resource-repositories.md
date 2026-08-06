---
title: "Resource Repositories"
description: "Technical reference for built-in resource repositories: supported access types, credential resolution, and capabilities."
weight: 6
toc: true
---

This page is the technical reference for built-in resource repositories. For an introduction to what resource
repositories are and why they exist, see [Concept: Resource Repositories]({{< relref "docs/concepts/resource-repositories.md" >}}).

---

## OCI Resource Repository

Handles OCI artifacts stored in OCI-compliant registries.

### Supported Access Types

| Access Type                                                            |
|------------------------------------------------------------------------|
| [`OCIImage/v1`]({{< relref "input-and-access-types.md" >}}#ociimagev1) |

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

The OCI resource repository also implements digest processing. When constructing a component version with a by-reference
resource, OCM queries the registry to resolve and verify the artifact's digest, ensuring the resource descriptor is
pinned to an immutable reference.

---

## Helm Resource Repository

Handles Helm charts stored in HTTP/HTTPS-based chart repositories.

### Supported Access Types

| Access Type                                                    |
|----------------------------------------------------------------|
| [`Helm/v1`]({{< relref "input-and-access-types.md" >}}#helmv1) |

### Capabilities

| Operation         | Supported |
|-------------------|-----------|
| Download          | Yes       |
| Upload            | No        |
| Digest Processing | Yes       |

{{< callout context="note" >}}
Upload is not supported because traditional Helm chart repositories are read-only HTTP servers that serve a static
`index.yaml` and packaged chart archives. There is no standardized upload API.

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
| `path`     | `podinfo`                |

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

### Digest Processing

The Helm digest processor resolves chart digests from the remote repository. For HTTP/HTTPS repositories it downloads
the `index.yaml` and extracts the digest for the specified chart and version. For OCI-based Helm repositories it
resolves the OCI manifest digest via the registry API.

---

## Wget Resource Repository

Handles resources served over plain HTTP or HTTPS.

### Supported Access Types

| Access Type                                                           |
|-----------------------------------------------------------------------|
| [`Wget/v1`]({{< relref "input-and-access-types.md" >}}#wgetv1-access) |

### Capabilities

| Operation         | Supported |
|-------------------|-----------|
| Download          | Yes       |
| Upload            | No        |
| Digest Processing | Yes       |

{{< callout context="note" >}}
Upload is not supported because a plain HTTP endpoint has no standardized write API. The transfer downloads the content
and stores it as a [`LocalBlob/v1`]({{< relref "input-and-access-types.md" >}}#localblobv1) in the target repository, regardless of the requested upload type.
{{< /callout >}}

### Credential Resolution

The credential consumer identity is derived from the `url` field in the access specification. The identity type is
`Wget`.

**Example:** For a resource with `url: https://downloads.example.com/myapp/1.0.0/myapp.tar.gz`:

| Attribute  | Value                      |
|------------|----------------------------|
| `type`     | `Wget`                     |
| `hostname` | `downloads.example.com`    |
| `scheme`   | `https`                    |
| `path`     | `myapp/1.0.0/myapp.tar.gz` |

The [`Wget/v1` input type]({{< relref "input-and-access-types.md" >}}#wgetv1-input) derives the identity the same way,
so one consumer entry covers construction and later downloads.

See [Credential Consumer Identities: Wget]({{< relref "credential-consumer-identities.md" >}}#wget) for matching rules.

### Download Behavior

Performs the request described by the access specification (`verb`, `header`, `body`, `noRedirect`) and returns the
response body as a file-backed blob. Only 2xx responses are accepted. The body is streamed to a file under the
`tempFolder` of the `filesystem.config.ocm.software/v1alpha1` configuration type rather than buffered in memory, and
there is no size limit by default. The media type of the blob is taken from `mediaType`, falling back to the response
`Content-Type` and then to `application/octet-stream`.

Request timeouts, retries, and per-host settings come from the
[HTTP client configuration]({{< relref "http-client-configuration.md" >}}).

### Digest Processing

The Wget digest processor downloads the referenced content and hashes it with SHA-256, using the
`genericBlobDigest/v1` normalisation. When the resource already carries a digest, the computed value is verified against
it and a mismatch fails the operation. Because the digest is computed over the fetched bytes, a URL whose content
changes will not verify against a previously recorded digest.

A resource can carry a digest before it has ever been fetched: setting the optional `digest` field on the resource in
`component-constructor.yaml` turns the recorded value into an assertion, so `ocm add cv` fails rather than recording
whatever the server returned. See
[Tutorial: Work with HTTP Resources]({{< relref "docs/tutorials/wget-http-resources.md#pin-a-digest" >}}).

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
