---
title: "Working with OCI"
description: "Embed OCI image layouts in component versions and access them natively from OCI registries after transfer."
icon: "🐳"
weight: 66
toc: true
---

In this tutorial, you'll learn how to embed OCI artifacts as local blobs inside component versions and transfer them to OCI registries where they become natively accessible — pullable with standard OCI tools like `docker pull`, `oras pull`, or `crane`.

## What You'll Learn

- Create an OCI image layout using the ORAS CLI
- Embed an OCI image layout as a local blob in a component version
- Transfer a component version with `--copy-resources` so that external OCI image references are internalized as local blobs
- Access local blobs natively from an OCI registry by their `localReference` digest and media type

**Estimated time:** ~20 minutes

## Scenario

You're packaging a microservice as an OCM component. The component includes a container image that must travel with the component — not just as a reference, but as an embedded artifact. When the component arrives at a target registry, you want the image to be pullable directly with standard OCI tooling, without needing the OCM CLI.

OCM v2 makes this possible through its [OCI-compatible index representation](https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/03-storage-backends/oci.md#62-index-representation). When a component version is stored in an OCI registry, native OCI artifacts (images, Helm charts, OCI image layouts) stored as local blobs are mapped to proper OCI manifests within the component version's index. This means they can be accessed directly by digest using any OCI-compliant client.

## How It Works

```mermaid
flowchart LR
    subgraph create ["Create"]
        direction TB
        ORAS["ORAS CLI"] --> Layout["OCI Image Layout (.tar)"]
        Layout --> Constructor["component-constructor.yaml"]
        Constructor --> CTF["CTF Archive"]
    end

    CTF --> Transfer["ocm transfer cv<br/>--copy-resources"]

    subgraph registry ["OCI Registry"]
        direction TB
        Index["Component Version<br/>(OCI Index)"]
        Manifest["OCI Image Manifest<br/>(native access)"]
        Index --> Manifest
    end

    Transfer --> Index

    Manifest --> Pull["docker pull / oras pull<br/>by digest"]

    style Pull fill:#dcfce7,color:#166534
```

The component version is stored as an OCI index. Local blobs with OCI media types are stored as separate OCI manifests within that index, making them natively accessible by their digest.

## Prerequisites

- [OCM CLI installed]({{< relref "docs/getting-started/ocm-cli-installation.md" >}})
- [ORAS CLI](https://oras.land/docs/installation) installed (for embedding OCI image layouts)
- [jq](https://jqlang.org/) installed (for inspecting JSON)
- Access to an OCI registry (e.g. a local registry via `docker run -d -p 5001:5000 registry:2`)

## Tutorial

{{< tabs "oci-use-cases" >}}

{{< tab "Embed an OCI Image Layout" >}}

In this use case, you create an OCI image layout from scratch using ORAS, embed it in a component version, and transfer it to an OCI registry where it becomes natively accessible.

{{< steps >}}

{{< step >}}

### Set up a working directory

```bash
mkdir -p /tmp/ocm-native-oci && cd /tmp/ocm-native-oci
```

{{< /step >}}

{{< step >}}

### Create a sample artifact with ORAS

Create a simple file and package it as an OCI artifact using ORAS. This produces an OCI image layout on disk:

```bash
# Create sample content
echo '{"message": "hello from OCM"}' > artifact.json

# Create an OCI image layout directory using ORAS
mkdir -p oci-layout
oras push --oci-layout oci-layout:latest \
  --artifact-type application/vnd.example.config \
  artifact.json:application/json
```

Verify the layout was created:

```bash
ls oci-layout/
```

You should see the standard OCI image layout structure:

```text
blobs/
index.json
oci-layout
```

{{< /step >}}

{{< step >}}

### Create the component constructor

Create a `component-constructor.yaml` that embeds the OCI image layout directory using the `dir/v1` input type:

```yaml
cat > component-constructor.yaml << 'EOF'
# yaml-language-server: $schema=https://ocm.software/latest/schemas/bindings/go/constructor/schema-2020-12.json
components:
- name: github.com/acme.org/native-oci-demo
  version: 1.0.0
  provider:
    name: acme.org
  resources:
    - name: my-oci-artifact
      type: ociArtifact
      version: 1.0.0
      input:
        type: dir/v1
        path: ./oci-layout
        mediaType: application/vnd.ocm.software.oci.layout.v1+tar
EOF
```

Key points:

- The `type: dir/v1` input packs the directory into a tar archive and embeds it by value as a local blob
- The `mediaType: application/vnd.ocm.software.oci.layout.v1+tar` tells OCM this is an OCI image layout, not an opaque blob — the `dir/v1` input automatically produces the tar archive expected by this media type
- During `ocm add cv`, OCM unpacks the layout and stores the contained manifest directly — the resulting local blob has the native OCI media type (e.g. `application/vnd.oci.image.manifest.v1+json`), not the tar media type

{{< /step >}}

{{< step >}}

### Build the component version

```bash
ocm add cv
```

<details>
<summary>Expected output</summary>

```text
 COMPONENT                          | VERSION | PROVIDER
------------------------------------+---------+----------
 github.com/acme.org/native-oci-demo | 1.0.0   | acme.org
```

</details>

{{< /step >}}

{{< step >}}

### Inspect the component version

Examine the component descriptor to see how the resource was stored:

```bash
ocm get cv ./transport-archive//github.com/acme.org/native-oci-demo:1.0.0 -o yaml
```

<details>
<summary>Expected output</summary>

```yaml
- component:
    name: github.com/acme.org/native-oci-demo
    provider: acme.org
    resources:
      - access:
          localReference: sha256:...
          mediaType: application/vnd.oci.image.manifest.v1+json
          type: LocalBlob/v1
        name: my-oci-artifact
        relation: local
        type: ociArtifact
        version: 1.0.0
    version: 1.0.0
  meta:
    schemaVersion: v2
```

</details>

Notice that the resource has `access.type: LocalBlob/v1` with the native OCI manifest media type. OCM recognized the OCI image layout during ingestion, unpacked it, and stored the manifest directly. The `localReference` contains the digest of the embedded manifest.

{{< /step >}}

{{< step >}}

### Transfer to an OCI registry

Transfer the component version to an OCI registry. The `--copy-resources` flag ensures all local blobs are transferred:

```bash
ocm transfer cv \
  --copy-resources \
  ./transport-archive//github.com/acme.org/native-oci-demo:1.0.0 \
  <your-registry>
```

Replace `<your-registry>` with your registry address (e.g. `http://localhost:5001` for a local HTTP registry, or `ghcr.io/my-org/ocm` for a remote HTTPS registry).

{{< callout context="note" title="Note" icon="outline/info-circle" >}}
For local registries running without TLS, use the `http://` scheme prefix (e.g. `http://localhost:5001`). HTTPS registries work without a scheme prefix.
{{< /callout >}}

During transfer, OCM stores the OCI manifest and its layers as native OCI objects in the registry. The component version's index references the manifest directly.

{{< /step >}}

{{< step >}}

### Access the image natively

After transfer, inspect the component version in the registry to find the image's digest:

```bash
ocm get cv <your-registry>//github.com/acme.org/native-oci-demo:1.0.0 -o yaml
```

Look for the `localReference` field in the resource's access specification — it contains the digest of the native OCI manifest:

```yaml
resources:
  - access:
      localReference: sha256:...
      mediaType: application/vnd.oci.image.manifest.v1+json
      type: LocalBlob/v1
    name: my-oci-artifact
    type: ociArtifact
```

You can pull the artifact natively using the `localReference` digest combined with the component version's repository path:

```bash
# Using ORAS
oras pull <your-registry>/component-descriptors/github.com/acme.org/native-oci-demo@sha256:...

# Using crane
crane pull <your-registry>/component-descriptors/github.com/acme.org/native-oci-demo@sha256:... image.tar
```

{{< callout context="note" title="Note" icon="outline/info-circle" >}}
The repository path follows the pattern `<registry>/<subpath>/component-descriptors/<component-name>`. The digest from `localReference` addresses the manifest directly within the component version's OCI index.
{{< /callout >}}

{{< /step >}}

{{< /steps >}}

{{< /tab >}}

{{< tab "Transfer by Value" >}}

In this use case, you start with a component version that references an external OCI image, transfer it with `--copy-resources` to internalize the image as a local blob, and then access it natively from the target registry.

{{< steps >}}

{{< step >}}

### Set up a working directory {#setup-transfer}

```bash
mkdir -p /tmp/ocm-transfer-native && cd /tmp/ocm-transfer-native
```

{{< /step >}}

{{< step >}}

### Create a component with an external image reference

Create a component that references an existing OCI image by reference (not by value):

```yaml
cat > component-constructor.yaml << 'EOF'
# yaml-language-server: $schema=https://ocm.software/latest/schemas/bindings/go/constructor/schema-2020-12.json
components:
- name: github.com/acme.org/transfer-demo
  version: 1.0.0
  provider:
    name: acme.org
  resources:
    - name: app-image
      type: ociImage
      version: 1.0.0
      access:
        type: ociArtifact
        imageReference: ghcr.io/stefanprodan/podinfo:6.9.1
EOF
```

Build the component version:

```bash
ocm add cv
```

{{< /step >}}

{{< step >}}

### Inspect the external reference

```bash
ocm get cv ./transport-archive//github.com/acme.org/transfer-demo:1.0.0 -o yaml
```

<details>
<summary>Expected output</summary>

```yaml
- component:
    name: github.com/acme.org/transfer-demo
    provider: acme.org
    resources:
      - access:
          imageReference: ghcr.io/stefanprodan/podinfo:6.9.1@sha256:...
          type: ociArtifact
        name: app-image
        relation: external
        type: ociImage
        version: 1.0.0
    version: 1.0.0
  meta:
    schemaVersion: v2
```

</details>

The image is referenced externally — it still lives in `ghcr.io`. The component descriptor only stores the reference.

{{< /step >}}

{{< step >}}

### Transfer with --copy-resources

Transfer the component to your target registry, copying all resources by value:

```bash
ocm transfer cv \
  --copy-resources \
  ./transport-archive//github.com/acme.org/transfer-demo:1.0.0 \
  <your-registry>
```

{{< callout context="note" title="Note" icon="outline/info-circle" >}}
For local registries running without TLS, use the `http://` scheme prefix (e.g. `http://localhost:5001`). HTTPS registries work without a scheme prefix.
{{< /callout >}}

With `--copy-resources`, OCM:

1. Downloads the image from `ghcr.io/stefanprodan/podinfo:6.9.1`
2. Stores it as a local blob in the target component version
3. Maps it to a native OCI manifest in the component version's index
4. Updates the access specification with a `localReference` (digest) pointing to the native OCI manifest

{{< /step >}}

{{< step >}}

### Inspect the transferred component

```bash
ocm get cv <your-registry>//github.com/acme.org/transfer-demo:1.0.0 -o yaml
```

After transfer with `--copy-resources`, the access specification changes from an external reference to a local blob:

```yaml
resources:
  - access:
      localReference: sha256:...
      mediaType: application/vnd.oci.image.index.v1+json
      referenceName: stefanprodan/podinfo:6.9.1
      type: LocalBlob/v1
    name: app-image
    relation: external
    type: ociImage
    version: 1.0.0
```

Key observations:

- `access.type` changed from `ociArtifact` to `LocalBlob/v1` — the image is now embedded
- `localReference` contains the digest of the stored image manifest/index
- `mediaType` is `application/vnd.oci.image.index.v1+json` (or `application/vnd.oci.image.manifest.v1+json` for single-platform images)
- `referenceName` preserves the original image reference for traceability
- `relation` remains `external` — this indicates the resource was originally sourced externally, even though it is now stored locally

{{< /step >}}

{{< step >}}

### Pull the image natively

Use the `localReference` digest to pull the image with standard OCI tooling. The image is stored within the component version's repository:

```bash
# Using docker
docker pull <your-registry>/component-descriptors/github.com/acme.org/transfer-demo@sha256:...

# Using crane
crane manifest <your-registry>/component-descriptors/github.com/acme.org/transfer-demo@sha256:...
```

The image is stored as a first-class OCI manifest in the registry. No OCM tooling is required to access it — any OCI-compliant client works.

You can also download through OCM:

```bash
ocm download resource <your-registry>//github.com/acme.org/transfer-demo:1.0.0 \
  --identity name=app-image \
  --output app-image-download
```

{{< /step >}}

{{< /steps >}}

{{< /tab >}}

{{< tab "Fetch from Remote with ORAS" >}}

In this use case, you fetch an existing OCI artifact from a remote registry using ORAS, embed it into a component version, and transfer it to a target registry.

{{< steps >}}

{{< step >}}

### Set up a working directory {#setup-fetch}

```bash
mkdir -p /tmp/ocm-fetch-oci && cd /tmp/ocm-fetch-oci
```

{{< /step >}}

{{< step >}}

### Fetch the OCI artifact with ORAS

Pull an existing artifact from a remote registry into a local OCI image layout:

```bash
mkdir -p oci-layout
oras copy ghcr.io/stefanprodan/podinfo:6.9.1 --to-oci-layout oci-layout:latest
```

{{< /step >}}

{{< step >}}

### Create the component constructor {#constructor-fetch}

```yaml
cat > component-constructor.yaml << 'EOF'
components:
- name: github.com/acme.org/fetched-oci-demo
  version: 1.0.0
  provider:
    name: acme.org
  resources:
    - name: app-image
      type: ociArtifact
      version: 1.0.0
      input:
        type: dir/v1
        path: ./oci-layout
        mediaType: application/vnd.ocm.software.oci.layout.v1+tar
EOF
```

{{< /step >}}

{{< step >}}

### Build and transfer

```bash
ocm add cv

ocm transfer cv \
  --copy-resources \
  ./transport-archive//github.com/acme.org/fetched-oci-demo:1.0.0 \
  <your-registry>
```

{{< /step >}}

{{< step >}}

### Inspect the transferred component version

After transfer, inspect the component version in the registry:

```bash
ocm get cv <your-registry>//github.com/acme.org/fetched-oci-demo:1.0.0 -o yaml
```

The resource access now shows a local blob reference:

```yaml
resources:
  - access:
      localReference: sha256:abc123...
      mediaType: application/vnd.oci.image.index.v1+json
      referenceName: stefanprodan/podinfo:6.9.1
      type: LocalBlob/v1
    name: app-image
    relation: external
    type: ociArtifact
    version: 1.0.0
```

Key observations:

- `localReference` contains the digest of the stored image manifest/index — this is the value you need for native access
- `mediaType` is `application/vnd.oci.image.index.v1+json` (multi-platform) or `application/vnd.oci.image.manifest.v1+json` (single-platform)
- `referenceName` preserves the original image reference for traceability

{{< /step >}}

{{< step >}}

### Pull natively

Use the `localReference` digest from the previous step to pull the artifact with standard OCI tooling:

```bash
# Pull using the localReference digest
oras pull <your-registry>/component-descriptors/github.com/acme.org/fetched-oci-demo@sha256:abc123...
```

The image is stored as a first-class OCI manifest in the registry. No OCM tooling is required to access it — any OCI-compliant client works.

{{< /step >}}

{{< /steps >}}

{{< /tab >}}

{{< /tabs >}}

## How Native Access Works

Under the hood, OCM v2 stores component versions as [OCI Image Indexes](https://github.com/opencontainers/image-spec/blob/main/image-index.md). When a local blob has an OCI-native media type (image manifest, image index, or OCI image layout), it is stored as a separate OCI manifest referenced from the component version's index — not as an opaque layer.

```mermaid
flowchart TB
    subgraph index ["Component Version (OCI Index)"]
        Desc["Descriptor Manifest<br/>software.ocm.descriptor=true"]
        NativeImg["Image Manifest<br/>(native OCI artifact)"]
    end

    Desc -- "layer" --> NonOCI["config.yaml<br/>(non-OCI blob)"]
    Desc -- "ocm download resource" --> OCM["OCM CLI consumer"]
    NativeImg -- "docker pull / oras pull / crane pull" --> Pull["Any OCI client"]

    style Pull fill:#dcfce7,color:#166534
    style OCM fill:#dbeafe,color:#1e40af
```

This means:

- **Non-OCI blobs** (plain files, config data) are stored as layers in the descriptor manifest, accessed only through OCM tooling
- **Native OCI artifacts** (images, Helm charts) are stored as separate manifests in the index, accessible both through OCM and directly through any OCI client

## Check Your Understanding

{{< details "Why does the media type matter when embedding an OCI image layout?" >}}
The media type (`application/vnd.ocm.software.oci.layout.v1+tar`) tells OCM that the blob contains a valid OCI image layout. During `ocm add cv`, OCM unpacks the tar, extracts the manifests and layers, and stores them as native OCI objects. The resulting local blob has the native OCI media type (e.g. `application/vnd.oci.image.manifest.v1+json`). Without the correct media type, OCM would store the tar as an opaque layer that cannot be accessed natively.
{{< /details >}}

{{< details "What is the difference between localReference and globalAccess?" >}}

- **`localReference`** is a content-addressable digest that identifies the blob within the component version's storage. It is **stable across transfers** — the same digest works regardless of which registry hosts the component, because it is derived from the blob content itself. It works with any OCM repository implementation (CTF archives, OCI registries). For native OCI artifacts stored in an OCI registry, you can access them directly using this digest combined with the component version's repository path.
- **`globalAccess`** is an optional, location-specific access specification that points to the artifact in a particular registry. It is **not set by default** — the `globalAccessPolicy` must be explicitly configured to enable it. Note that this reference **becomes invalid after mirroring** — when the component is transferred to a different registry, the `globalAccess` still points to the original registry. Always use `localReference` for stable, location-independent access.

{{< /details >}}

{{< details "How do I enable globalAccess references?" >}}
By default, `globalAccess` is not populated. To opt in, use the two-step transfer workflow with a transfer specification:

1. Generate the transfer spec:

   ```bash
   ocm transfer cv --dry-run -o yaml --copy-resources \
     ./transport-archive//github.com/acme.org/native-oci-demo:1.0.0 \
     <your-registry> > spec.yaml
   ```

2. Edit `spec.yaml` and add `globalAccessPolicy: auto` to each `OCIAddLocalResource` node's `spec` field:

   ```yaml
   - type: OCIAddLocalResource/v1alpha1
     spec:
       globalAccessPolicy: auto
       # ... other fields ...
   ```

3. Execute the modified spec:

   ```bash
   ocm transfer cv --transfer-spec spec.yaml
   ```

This is an experimental feature carried over from OCM v1 for backwards compatibility. Its future availability is being evaluated by the community.

With `globalAccessPolicy: auto`, the descriptor looks like this after transfer:

```yaml
resources:
  - access:
      localReference: sha256:abc123...
      mediaType: application/vnd.oci.image.index.v1+json
      referenceName: stefanprodan/podinfo:6.9.1
      type: LocalBlob/v1
      globalAccess:
        imageReference: <your-registry>/component-descriptors/github.com/acme.org/native-oci-demo@sha256:abc123...
        type: ociArtifact
    name: app-image
    relation: external
    type: ociArtifact
    version: 1.0.0
```

The `globalAccess.imageReference` provides a direct pullable reference. Note that this reference may become stale if the component is transferred to another registry using tooling that does not update the `globalAccess` field.

Without `globalAccess`, you can still access native OCI artifacts directly using the `localReference` digest and the component version's repository path in the registry.
{{< /details >}}

{{< details "Can I use `--upload-as` to control how artifacts are stored?" >}}
Yes. The `ocm transfer cv` command supports `--upload-as` with two values:

- `--upload-as localBlob` — stores OCI artifacts as local blobs within the component version (default behavior with `--copy-resources`)
- `--upload-as ociArtifact` — uploads OCI artifacts as standalone OCI artifacts in the target registry, separate from the component version

Both options make the artifact natively accessible in OCI registries, but `localBlob` keeps the artifact within the component version's index while `ociArtifact` stores it independently.
{{< /details >}}

## Cleanup

Remove the tutorial artifacts:

```bash
rm -rf /tmp/ocm-native-oci /tmp/ocm-transfer-native /tmp/ocm-fetch-oci
```

## Next Steps

- [How-To: Transfer Components Across an Air Gap]({{< relref "docs/how-to/air-gap-transfer.md" >}}) — Transfer signed components through air-gapped environments
- [How-To: Download Resources from Component Versions]({{< relref "docs/how-to/download-resources-from-component-versions.md" >}}) — Extract resources from components

## Related Documentation

- [Concept: Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}}) — Understand resource handling during transfer
- [Reference: Input and Access Types]({{< relref "docs/reference/input-and-access-types.md" >}}) — All supported resource types
- [OCM OCI Storage Spec: Index Representation](https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/03-storage-backends/oci.md#62-index-representation) — How component versions map to OCI indexes
