# Next Generation Component Constructor Support

* **Status**: proposed  
* **Deciders**: OCM Technical Steering Committee
* **Date**: 2025.07.09  

**Technical Story**:  
Enable easy and simple interpretation of downloaded resources fetched from component versions based on a new set of specifications that integrates seamlessly with the new OCM Plugin System, while providing a newly architected foundation from the OCM v1 download handlers.

---

## Context and Problem Statement

Whenever users of OCM want to fetch resources / sources from component versions, the returned interface is a `blob.ReadOnlyBlob` that can be used to download the resource / source data based on the access. This is a simple and effective way to handle binary streams, especially when working with the OCM CLI or other tools that directly interact with the OCM API.

However, when working within the context of a CLI, the user experience is not as straightforward. There is a need for a more user-friendly approach to not only download the data behind the resource in a binary stream, but also to interpret it correctly, e.g., for extraction into a filesystem, especially for those who may not be familiar with the underlying API calls in the Go API directly.

As such, we want to continue supporting commands in the form of:

```shell
ocm download resources <component-reference> <resource-reference>
```

where `<component-reference>` is a reference to a component version and `<resource-reference>` is a reference to a resource within that component version.

This requires the support of a new set of handlers that can reuse the existing OCM binding libraries to interpret the resource data and provide a more granular download infrastructure.

To specify the exact download that is required, we introduce a new concept called a **BlobTransformer**, that is simpler than the existing "download handler" concept, but still allows for a flexible and extensible way to handle resource data.

This will allow us to define how the resource data should be transformed before it is returned to the user — e.g., extracting a tarball, converting a JSON file, etc.

In the OCM CLI, users will be able to transform their downloaded resource by specifying a `BlobTransformer` on the command line:

```shell
ocm download resources <component-reference> <resource-reference> --transform extract-oci-artifact
```

This design continues the philosophy of:

* Maintaining the format and structure of choosing a handler with specific behavior, with a possible set of defaults (e.g., preconfigured handlers are replaced with `BlobTransformer` implementations).
* Ensuring that transformers can be registered dynamically (via the plugin manager) or inbuilt (via the OCM CLI)
* Reusing the existing binding libraries to interact with OCM repositories (by interacting only with `blob.ReadOnlyBlob`)

**Important Note**: The `BlobTransformer` does *not* work on the Resource/Source level. It is intentionally independent of the resource description or access method. Instead, the blob’s MIME-type or other self-declared metadata is used to guide transformation logic. This is because it is expected that the transformation should work on any data stream independent of resources or sources.

---

## Decision Drivers

* **Simplicity of Architecture**: The model avoids over-engineering and keeps resource transformation localized and decoupled.
* **Extensibility**: The plugin-based approach allows for new transformers to be added without changing the core CLI logic.
* **Maintainability**: Clear interfaces and a separation of concerns will reduce technical debt and allow for more robust testing and evolution of the system.

---

## Outcome

We will implement a plugin-driven, extensible transformation system based on the `BlobTransformer` contract that supports interpreting blobs in the CLI, without requiring custom user code.

---

## An Easy-to-Use `bindings/go/blob/transformer` Library

The core of the proposed system is a new module:

```text
bindings/go/blob/transformer
```

This module is responsible for registering and invoking `BlobTransformer` implementations, whether inbuilt or provided by plugins. It acts as a central point for managing transformation logic and supporting configuration resolution.

---

## `BlobTransformers` and Contracts

The key interface provided by this module is:

```go
package transformer

import (
    "context"

    "ocm.software/open-component-model/bindings/go/blob"
    "ocm.software/open-component-model/bindings/go/runtime"
)

type BlobTransformer interface {
    // TransformBlob transforms the given blob data according to the specified configuration.
    // It returns the transformed data as a blob.ReadOnlyBlob or an error if the transformation fails.
    TransformBlob(ctx context.Context, input blob.ReadOnlyBlob, config runtime.Typed) (blob.ReadOnlyBlob, error)
}
```

This allows flexible input and output handling based solely on `blob.ReadOnlyBlob`, enabling powerful chaining and reuse.

---

### Example Configuration

The transformer system will look for a `transformer.blob.ocm.software/v1alpha1` config in the `.ocmconfig` file:

```yaml
type: transformer.blob.ocm.software/v1alpha1
transformers:
- id: extract-oci-artifact
  spec:
    type: extract.oci.artifact.ocm.software/v1alpha1
    # additional configuration for the transformer
```

This allows transformer registration via configuration, enabling portable setups and declarative ocm configuration structures.
We can achieve this by registering all configurations by their `id` and referencing those identities in the CLI commands.

At the same time, we are able to define a set of default transformers in the CLI that are added on top and can be documented.

### Using the `BlobTransformer` Infrastructure for OCM CLI

The OCM CLI will leverage the `BlobTransformer` interface to provide a consistent and user-friendly experience for downloading and transforming resources. 
Users can specify the desired transformation directly in their CLI commands, allowing for seamless integration with existing workflows.
The CLI will support any transformer that returns a `blob.ReadOnlyBlob` with the following cases:

- MediaType is `application/tar` or `application/tar+gzip` for tarball extraction: The given tar will be extracted into the given destination directory.
- MediaType is different from above, or unknown: we will leverage the MIME-type to determine the appropriate file suffix and unpack the file as is into the given destination directory.

This allows us to provide a unified interface for resource downloads while still allowing for flexibility in how the data is handled.

### Transforming OCI Layouts with `extract.oci.artifact.ocm.software/v1alpha1`

The most common use case for interacting with OCI artifacts is to extract the contents of a specific OCI artifact downloaded into an OCI Layout presented as a tarball stream in the `blob.ReadOnlyBlob`.

For extracting such artifacts, we will offer the default `extract.oci.artifact.ocm.software/v1alpha1` transformer, which will handle the extraction of OCI artifacts from a tarball stream.

By default, this transformer will:

1. Introspect the tarball stream to identify the OCI Layout, and access its index.
2. Identify the top-level artifact within the OCI Layout, by introspecting the index.
3. Extract the artifact's content into a specified destination directory, preserving the OCI Layout structure.

By default, the transformer will extract all layers from the top-level artifact and unpack it into the specified destination directory, while trying to respect the original media type of the layer.

#### Example transformation command

Without Transformation:

```shell
ocm download resources ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.26.0 -o ./ --identity name=ocmcli,os=linux,arch=amd64
```

*This will download the `ocmcli` binary as a binary stream, which can be run as is.*

With Transformation

```shell
ocm download resources ghcr.io/open-component-model/samples//sample-chart -o ./ --identity name=podinfo --transform extract-oci-artifact 
```

*This will trigger the `extract.oci.artifact.ocm.software/v1alpha1` transformer*, which will:

1. Download the `sample-chart` resource as a tarball stream.
2. Extract the contents of the OCI Layout into a specified destination directory (e.g., `./sample-chart`).
3. Because the first layer is a Helm Chart it will extracted as a unpacked chart.

---

## Processing Architecture

1. **Input**: Resource blob returned from the repository.
2. **Dispatch**: Transformer type is resolved based on user input or default configuration.
3. **Execution**: Transformer plugin is invoked with the blob and its configuration.
4. **Output**: Transformed blob is returned or unpacked into a target destination (e.g., filesystem).

---

## Pros and Cons

### Pros

* ✅ Decouples resource interpretation from CLI logic.
* ✅ Supports future CLI features like chaining transformers or inspecting outputs.
* ✅ Enables plugin authors to write custom resource transformations without touching core code.
* ✅ Retains full compatibility with existing `blob.ReadOnlyBlob` handling.
* ✅ Provides a consistent, type-safe interface for resource transformation that significantly simplifies the CLI user experience.

### Cons

* ❌ Adds initial complexity for new users unfamiliar with transformation configuration.
* ❌ Plugin ecosystem may need time to mature and stabilize.
* ❌ Requires developers to adhere to MIME-type maintenance discipline for best results.

---

## Conclusion

By introducing the `BlobTransformer` abstraction and associating it with a plugin-capable transformer library, OCM can support a more powerful and flexible CLI experience that is consistent, maintainable, and easily extensible. This empowers both CLI users and tool developers to work with resource blobs in more expressive and automated ways — from tar extraction to content conversion — while preserving the underlying API simplicity and type safety.
