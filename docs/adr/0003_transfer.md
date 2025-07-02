# A Specification for Transferring OCM Components

* Status: proposed
* Deciders: Gergely Brautigam, Fabian Burth, Jakob Moeller
* Date: 2025-02-17

Technical Story: Design a specification for ocm components that allows to
transfer components and their resources from multiple source locations to
multiple target locations.

## Table of Contents

## Context and Problem Statement

An ocm component typically models a product as the collection of all software of
all its _software artifacts_. To deploy a product, all the software artifacts
comprising that product might need to be transferred to **one or multiple**
target locations (e.g. a private cloud environment).

One might imagine multiple transfer scenarios:

### Scenarios on Component Level

* **1 source-location : n target-locations** - The components are owned and
  built by teams within the same organization. In this case, all the components
  comprising that product are typically stored in a single ocm repository. Thus,
  the components need to be transferred from one source location to multiple
  target locations.
* **n source-locations : m target-locations** - The components are owned and
  built by teams in different organizations. In this case, the components
  comprising that product are typically stored in multiple ocm repositories.
  Thus, the components need to be transferred from multiple source locations to
  multiple target locations.

### Scenarios on Resource Level

Resources typically have to be stored in particular technology specific storage
systems to be deployed by respective technology specific deployment tools
(e.g. to deploy oci images to kubernetes, the images need to be stored in an oci
registry). Thus, for a component transfer, one might imagine multiple scenarios
for a resource:

* **transformation and upload**
  * **local blob** - The resource might be packaged with the component as a
      local blob. That local blob has a different format than the format
      required by the storage system in the target location (e.g. a helm chart
      stored as tar.gz that shall be transported to an oci registry).
  * **different storage system** - The resource might be stored in a different
      storage system than required by the respective technology specific
      deployment tools in the target location (e.g. a helm chart stored in a
      helm repository that shall be transported to an oci registry).
* **upload**
  * **local blob** - The resource might be packaged with the component as a
      local blob. The local blob has the same format as required by the storage
      system in the target location (e.g. a helm chart stored as an oci artifact
      that shall be transported to an oci registry).
  * **same storage system** - The resource might be stored in the same storage
      system as required by the respective technology specific deployment tools
      in the target location (e.g. a helm chart stored in an oci registry that
      shall be transported to an oci registry).

## Decision Drivers

* flexibility in transferring components and resources
* debugging ability and reproducibility/transparency of transfers
* separation of concerns between
  [component specification](https://github.com/open-component-model/ocm-spec)
  and transfer information (e.g. the repository name of a resource that shall be
  uploaded to an oci registry)
* simplicity for simple transfer scenarios
* extensibility of the transfer system to support complex custom transfer
  scenarios

## Considered Options

1. **[Option 1](#option-1-transfer-specification): Transfer Specification**
2. **[Option 2](#option-2-ocm-v1-transfer-handler): OCM v1 Transfer Handler**

## Option 1: Transfer Specification

Assume, we want to transfer the component described by the following
component descriptor:

```yaml
meta:
  schemaVersion: v2
component:
  name: ocm.software/root-component
  version: 1.0.0
  provider: ocm.software
  resources:
    - name: podinfo-image
      relation: external
      type: ociImage
      version: 1.0.0
      access:
        type: ociArtifact
        imageReference: ghcr.io/stefanprodan/podinfo:6.7.1
    - name: podinfo-chart
      relation: local
      type: helmChart
      version: 1.0.0
      access:
        type: localBlob
        localReference: sha256:ee03b550efa3fe87e3e2471d407de4ded833969112f2f40a628f9c3716666cef
        mediaType: application/vnd.oci.image.manifest.v1+tar+gzip
        referenceName: ocm.software/root-component/podinfo-chart:6.7.1
```

We want the component to be uploaded to `ghcr.
io/open-component-model/transfer-target`, the podinfo-image to be uploaded
to `ghcr.io/open-component-model/transfer-target/podinfo-image:1.0.0`, and
the podinfo-chart to be uploaded to `ghcr.
io/open-component-model/transfer-target/podinfo-chart:1.0.0`.

### Specification

```yaml
metadata:
  version: v1alpha1
spec:
  mappings:
    - component:
        name: github.com/acme.org/helloworld
        version: 1.0.0
      source:
        type: CommonTransportFormat
        filePath: /root/user/home/ocm-repository
      target:
        type: OCIRegistry
        baseUrl: ghcr.io
        subPath: open-component-model/transfer-target
      resources:
        - resource:
            name: podinfo-image
          transformations:
            - type: uploader.oci/v1alpha1
              imageReference: ghcr.io/open-component-model/transfer-target/podinfo-image:1.0.0
        - resource:
            name: podinfo-chart
          transformations:
            - type: localblob.to.oci/v1alpha1
            - type: uploader.oci/v1alpha1
              imageReference: ghcr.io/open-component-model/transfer-target/podinfo-chart:1.0.0
...
```

> **NOTES:**
>
> * **Sources:** The specification also support sources (analogous to
> resources). They are omitted here for brevity.
> * **Multiple Components:** The mappings property is a list. This allows
> transferring multiple components in one transfer operation based on a
> single transfer spec.

This specification contains all the information necessary to perform a transfer:

* Source and target location of the component
* Target location of the resources (and sources, if any)
* Transformations required to perform the upload to the target location such as:
  * format adjustments (e.g. local blob to oci artifact)
  * [localization](./0004_localization_at_transfer_time.md)

The transformation are implemented as plugins.

* The properties such as `imageReference` are passed to the plugin specified
  by the `type` as configuration.
* The byte stream of the resource content is passed from each transformation
  to the next transformation forming a pipeline.
* Besides the resource content, each transformation can also edit the
  resource specification in the component descriptor (e.g. adjust the digest
  after a format change or add a label)

> **NOTE:** The transformations are significantly more powerful than shown
> here. But this part should suffice to illustrate the concept for the basic
> transfer behavior.  
> For details about the transformation contract and implementation, refer to
> [the localization adr here](./0004_localization_at_transfer_time.md).

**Pro**

* **Reusable (custom) transformations** - Since the transformations are exposed
  on the API, users can define their own custom transformations or reuse other
  transformations (like github actions). This might be a _significant
  value-add_.
* **Clean formalization of transformation pipelines** - Exposing the
  transformations as an API forces a formalized and clean definition.
* **Localization as yet another transformation** - Localization can be
  implemented and exposed in the transfer spec as just another transformation.
* **Multiple upload targets** - The transfer spec allows for multiple upload
  targets for a resource.

**Con**

* **Complexity** - A lot of custom transformations might make it hard to
  understand what is happening in the transfer.
* **Hard to generate**

### Usage

1. **Transfer based on existing transfer spec:**
    
    ```bash
    ocm transfer --transfer-spec ./transfer-spec.yaml
    ```

    This command will transfer the component and its resources based on a defined
    transfer spec.

2. **Transfer based on dynamically generated transfer spec:**

    ```bash
    ocm transfer component [<options>] \
    ctf::/root/user/home/ocm-repository//ocm.software/component \
    ghcr.io/open-component-model/ocm-v1-transfer-target
    ```  

    This command mimics the old ocm v1 transfer command. It will provide the
    known options such as `--copy-resources` and `--recursive`. In the
    background, we will implement an opinionated generation of a transfer spec
    that essentially models the old transfer behavior.

### Considerations

#### Specification: Source Locations for Component but no Source Locations for

Resources The transfer spec currently includes the `source` location of the
components but not the source location of the resources.

* **Components:**  
  The transfer specification includes the source location of a component. This
  location is given as a command line input or in a config file.
* **Resources:**  
  The resource’s source location is already defined in the component
  descriptors.
* **Benefit:**  
  This approach makes the transfer specification the single source of truth for
  a transfer.

#### Generation of the Transfer Specification as part of the Transfer Command

The generation of the transfer spec (usage 2) will likely be part of the `ocm
transfer` command.

* **Avoid Fetching Descriptors Twice:**  
  The orchestrator will fetch component descriptors once and cache them. This
  avoids fetching them again during the transfer command.
* **--dry-run**:  
  To leverage the advantage of the cached component descriptors and still be
  able to run the generation independently of the transfer, it is intended to
  offer a `--dry-run` option to the `ocm
  transfer` command.

    ```bash
    ocm transfer component ctf::./ocm-repository//ocm.software/component \
    --copy-resources --recursive --dry-run 
    ```

## Option 2: OCM v1 Transfer Handler

This is the approach represents the `ocm transfer` command of the current ocm
cli.

```bash
ocm transfer component ctf::./ocm-repository//ocm.software/component \
  ghcr.io/open-component-model/ocm-v1-transfer-target
```

### Considerations

#### Single Target Location Only

The current version of ocm (ocm v1) only supports transferring components from
multiple source locations to a single target location. To look up components in
multiple ocm repositories with the above command, the user either has to use the
`--lookup` flag or specify a list of resolvers in the config file. If a
component cannot be found in the specified target repository, the lookup
repositories (aka resolvers) _will be iterated through_ as fallbacks which is
also rather inefficient (see
`ocm transfer --help` or `ocm configfile` documentation for more details).
_There is no way to configure multiple targets for a single component transfer._

#### Limited Control Over Resource Transfer

* **No fine-grained control over WHICH resources to transfer**  
  Essentially, there are 3 modes for resource transfer:
  * _Without an additional flag_, the command only copies the component
      descriptors and the local blobs.
  * _With the `--copy-local-resources` flag_, the command copies only the
      component descriptors, the local blobs, and all resources that have the
      relation `local`.
  * _With the `--copy-resources` flag_, the command copies all the resources
      during transfer. -
  > **NOTE:** Without **uploaders** registered, all the above option lead to all
  resources being transferred as a local blob - no matter the source storage
  system. For those wondering, that they never actively configured an uploader
  but their oci artifacts still ended up back in the target oci registry - that
  is because there is an _oci uploader_ configured by default.
* **No fine-grained decision on WHERE to transfer resources**  
  The resources are converted to local blobs during the transfer by default. To
  change this behavior and instead upload a resource to a particular target
  storage system during transfer, the user has to register so called
  [**uploadhandlers**](#upload-handlers) (for further details, see
  [documentation](https://ocm.software/docs/reference/ocm-cli/help/ocm-uploadhandlers/)).
  This can be done through the flag `--uploadhandler` or by specifying an
  uploader configuration in the config file.

* **No cross storage system / cross format transfers**-
  * The current version of the ocm implementation does not give the uploader
      implementations the possibility to edit the resource, only the resource
      access.
  * A cross storage system transfer requires a transformation of the resource
      content. This typically leads to a changed digest that has to be reflected
      in the component descriptor. Since the digest is part of the resource but
      not part of the access, this is currently not possible.

* **No transformers**  
  To actually enable the cross storage system transfer, the resource contents
  format typically has to be adjusted. In the current architecture, to enable
  this, each uploader would have to know all possible input formats itself.

* **No concept to specify target location information for resources**
  * In the [uploader handler](#upload-handlers) config below, it is mentioned
      that the resources matching the registration would be uploaded to oci with
      the prefix `https://ghcr.io/open-component-model/oci`. _But what is the
      resource specific suffix?_
  * Currently, there is a field called
      `hint` in the local blob access. If oci resources are downloaded into a
      local blob and then re-uploaded to oci, this hint is used to preserve the
      original repository name. These hints are not sufficient to fulfill the
      requirements of ocm (there are
      open [issues](https://github.com/open-component-model/ocm/issues/935)
      and [proposals](https://github.com/open-component-model/ocm/issues/1213))

* **No separation of concerns between ocm spec and transfer**  
  The transfer is supposed to be an operation ON TOP of the ocm spec. Thus,
  additional information required by a transfer should not pollute the ocm spec.
  Essentially, this is already violated by the current `hint` but would be
  completely broken through the
  current [proposal](https://github.com/open-component-model/ocm/issues/1213)
  on how to resolve the issue mentioned in the previous point.

* **Uploader Mechanism is implicit, non-transparent and hard to reproduce**

### Upload Handlers

Uploaders (in the code and repository also known as blobhandler) can be
registered for any combination of:

* _resource type_ (NOT access type)
* _media type_
* _implementation repository type_ - If the corresponding component is uploaded
  to an oci ocm repository, the implementation repository type is `OCIRegistry`.
  Another implementation repository type is `CommonTransportFormat`.

Additionally, the uploaders can be assigned a _priority_ to resolve conflicts of
the registration of multiple uploaders matches the same resource.

```yaml
type: uploader.ocm.config.ocm.software
handlers:
  - name: ocm/ociArtifacts
    artifactType: ociArtifact
    # media type does not make a lot of sense for oci artifacts, it improves 
    # the clarity of the registration example
    mediaType: application/vnd.oci.artifact
    repositoryType: OCIRegistry
    priority: 100 # this is the default priority
    config:
      ociRef: https://ghcr.io/open-component-model/oci
```

The config section in the upload handler registration depends on the type of
uploader being registered. The `ociRef` in the above example would mean that all
the resources that match this registration would be uploaded under the prefix
`https://ghcr.io/open-component-model/oci`.

## Decision Outcome

Chosen option: [Option 1](#option-1-transfer-specification), because:

* **Reusable Transformation Pipelines:**  
  These pipelines are valuable and work well with the transformation logic
  needed for localization during transfers.

* **Improve Debugging:**  
  The transfer specification makes it easier to debug and reproduce transfers.

* **Current Limitations:**  
  The existing transfer mechanism does not meet OCM requirements. It is too
  implicit and hard to understand, debug, and reproduce.

## Links <!-- optional -->
