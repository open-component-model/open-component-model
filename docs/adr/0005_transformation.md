# A Specification for Orchestration of OCM Component Operations

* Status: proposed
* Deciders: Gergely Brautigam, Fabian Burth, Jakob Moeller
* Date: 2025-02-17

Technical Story: Design a specification that enables the orchestration of
arbitrary operations on components and their resources.

> **NOTE:** This proposal essentially introduces **OCM as K8S + KRO for
> SBOMs**.  
> The _operation plugins_ are analogous to _k8s controllers_ (without the
> reconciliation). Their _types and the corresponding configuration schemas_ are
> analogous to the _k8s resource definitions_.   
> The analysis of CEL expressions to build up a directed
> acyclic graph (DAG) of operations is analogous to KROs _resource graph
> definition_.
>
> The ideas in this proposal are heavily inspired by [kro](https://kro.run/).
> The current proof-of-concept implementation even uses big parts of their code!

## Context and Problem Statement

The core use case the specification is designed for is the **transfer of
components and their resources**.

### Requirements

- **Transfer components and their resources from one or multiple source
  repositories to one or multiple target repositories**

  **Example:**
  - root component `ocm.software/root-component:1.0.0` is stored at `ghcr.
    io/ocm-component-model/transfer-source/ocm.software/root-component:1.0.0`
  - root component references `ocm.software/leaf-component:1.0.0`
  - leaf component is stored at
    `quay.io/ocm-component-model/transfer-source/ocm.software/leaf-component:1.0.0`

  Both components should be transferred to
  `ghcr.io/ocm-component-model/transfer-target`
  and `quay.io/ocm-component-model/transfer-target` in a single transfer
  process.


- **Transfer resources between different storage systems**

  **Example:**
  - resource `ocm-cli:1.0.0` is stored as an oci artifact at
    `ghcr.io/ocm-component-model/transfer-source/ocm-cli:1.0.0`

  Resource `ocm-cli:1.0.0` should be transferred to the central maven repository
  `https://repo1.maven.org/maven2` with the `GAV` `software.
  ocm/ocm-cli/1.0.0`. Thereby, the resource has to be transformed to a maven
  artifact.


- **Localize resources that are deployment instructions during transfer**

  **Example:**
  - component `ocm.software/root-component:1.0.0` contains a resource
    `ghcr.io/ocm-component-model/transfer-source/ocm-controller-deployment-manifest:1.0.0`
    and a resource
    `ghcr.io/ocm-component-model/transfer-source/ocm-controller:1.0.0`
  - resource `ocm-controller-deployment-manifest:1.0.0` is a k8s deployment and
    specifies
    `image: ghcr.io/ocm-component-model/transfer-source/ocm-controller:1.0.0`
    in its pod template
  - `ocm.software/root-component:1.0.0` and all its resources are transferred to
    a registry in a private environment to
    `private-registry.com/ocm-component-model/transfer-target/ocm
    -controller-deployment-manifest:1.0.0` and `private-registry.
      com/ocm-component-model/transfer-target/ocm-controller:1.0.0`

  The be able to consume the component in the private environment, the pod
  template in the deployment manifest has to be adjusted from `image: 
  ghcr.io/ocm-component-model/transfer-source/ocm-controller:1.0.0` to `image: 
  private-registry.com/ocm-component-model/transfer-target/ocm-controller:1.0.0`


- **Hash and sign components (during transfer)**

  **Example:**
  - component `ocm.software/root-component:1.0.0` references component
    `ocm.software/leaf-component:1.0.0`.
  - therefore, the _hash_ of component `ocm.software/root-component:1.0.0`
    incorporates the _hash_ of component `ocm.software/leaf-component:1.0.0`.

  Thus, the hash of `ocm.software/leaf-component:1.0.0` has to be calculated
  before the hash of `ocm.software/root-component:1.0.0` can be calculated.
  Since the hash of the resources content is part of the component hash,
  _cross storage system transfers_ and _localization_ require the hash to be
  recalculated during transfer.

### Conclusion

**Extensibility**: The **cross storage system transfer** and the
**localization** require the operations to be performed to be extensible.

- the transformation logic for re-packaging for cross storage system transfers
  depends on the source and target storage system (e.g. oci, maven, npm)
- the transformation logic for localization depends on the deployment
  description format (e.g. manifest, kustomization, helm)

**Ordering**: The **hash and sign** require the operations to be performed in a
specific order (child before parent components). Besides, there are several
other operations that either kind of **implicitly** depend on each other (data
flow between download resource and upload resource) or **explicitly** depend on
each other (localization needs the location of the image after transfer).

Also, users might want to incorporate their own operations:

- **filtering** image layers or entire resources based on target location of the
  transfer (e.g. for customer deliveries)

## Solution Proposal

An _ocm orchestration specification_ is a formalized description of operations
that have to be performed on components and their resources. It uses a **CEL
expression syntax** to determine dependencies between operations. Based on the
dependencies, a **directed acyclic graph (DAG)** is built up that determines the
order of operations.

In fact, the description format is currently so generic that it can be used to
orchestrate arbitrary operations on arbitrary data - essentially establishing is
**general purpose CEL based pipeline language**.

This allows to prepare or enrich operations on components and resources with
additional information.

### Example: OCM orchestration specification

Assume, we have the following components stored in
`ghcr.io/fabianburth/source-ocm-repository`:

```yaml
meta:
  schemaVersion: v2
component:
  name: ocm.software/root-component
  version: 1.0.0
  provider: ocm.software
  resources:
    - access:
        imageReference: ghcr.io/fabianburth/source-charts/podinfo:6.7.1
        type: ociArtifact
      name: mychart
      relation: external
      type: helmChart
      version: 6.7.1
    - access:
        imageReference: ghcr.io/fabianburth/source-image/podinfo:6.7.1
        type: ociArtifact
      name: myimage
      relation: external
      type: ociImage
      version: 6.7.1
  componentReferences:
    - name: leaf
      componentName: ocm.software/leaf-component
      version: 1.0.0
---
meta:
  schemaVersion: v2
component:
  name: ocm.software/leaf-component
  version: 1.0.0
  provider: ocm.software
  resources:
    - access:
        localReference: sha256:d7952ffc553c8f25044b4414fc40e1919d904b9bbc9a50e4d8aae188dabe4dba
        mediaType: application/vnd.oci.image.index.v1+tar+gzip
        referenceName: ocm.software/leaf-component/ocmcli-image:0.21.0
        type: localBlob
      name: ocmcli-image
      relation: external
      type: ociImage
      version: 1.0.0
```

We want to transfer the components to
`ghcr.io/fabianburth/target-ocm-repository/*`  and the resources to `ghcr.
io/fabianburth/target-*`. Thereby, we want to **localize the helm chart** and
**transform a local blob to an oci artifact**.

We want the component to be uploaded to `ghcr.
io/open-component-model/transfer-target`, the podinfo-image to be uploaded to
`ghcr.io/open-component-model/transfer-target/podinfo-image:1.0.0`, the
podinfo-chart to be uploaded to
`ghcr. io/open-component-model/transfer-target/podinfo-chart:1.0.0`, and the 
ocmcli-image to be uploaded to `ghcr.io/open-component-model/transfer-target/ocmcli-image:0.21.0`.

```yaml
type: transformation.ocm.component/v1alpha1
transformations:
  - type: attributes.transformation/v1alpha1
    id: constants
    attributes:
      targetFilePath: "./test/localization-multi-component/archive-after-localization"
  # component 1
  - type: downloader.component.ctf/v1alpha1
    id: downloadcomponent1
    name: github.com/acme.org/helloworld
    version: 1.0.0
    filePath: ./test/localization-multi-component/archive
  # resource 1
  - type: downloader.resource.oci/v1alpha1
    id: resourcedownload1
    componentDescriptor: ${downloadcomponent1.outputs.descriptor}
    resource:
      name: myimage
  - type: uploader.resource.oci/v1alpha1
    id: resourceupload1
    imageReference: ghcr.io/fabianburth/images/myimage:after-localize
    data: ${resourcedownload1.outputs.data}
  # resource 2
  - type: downloader.resource.oci/v1alpha1
    id: resourcedownload2
    componentDescriptor: ${downloadcomponent1.descriptor}
    resource:
      name: mychart
  - type: oci.to.tar.transformer/v1alpha1
    id: ocitotar1
    data: ${resourcedownload2.outputs.data}
  - type: yaml.engine.localization/v1
    id: localization1
    data: ${ocitotar1.outputs.data}
    file: "*/values.yaml"
    mappings:
      - path: "image.repository"
        value: "${resourceupload1.resource.access.imageReference.parseRef().
      registry}/${resourceupload1.resource.access.imageReference.parseRef().
      repository}"
  - type: tar.to.oci.transformer/v1alpha1
    id: tartooci1
    manifest: ${ocitotar1.outputs.manifest}
    ref: ${ocitotar1.outputs.ref}
    configLayer: ${ocitotar1.outputs.configLayer}
  - type: uploader.resource.oci/v1alpha1
    id: resourceupload2
    imageReference: ghcr.io/fabianburth/charts/myimage:after-localize
    componentDescriptor: ${downloadcomponent1.outputs.descriptor}
    data: ${tartooci1.outputs.data}

  - type: uploader.component.ctf/v1alpha1
    filePath: ${constants.spec.attributes.targetFilePath}
    data: ${downloadcomponent1.outputs.data}
  # component 2
  - type: downloader.component.ctf/v1alpha1
    id: downloadcomponent2
    name: github.com/acme.org/helloeurope
    version: 1.0.0
    filePath: ./test/localization-multi-component/archive
  # resource 3
  - type: downloader.localblob.ctf/v1alpha1 # we are overwriting our dependency here
    id: downloadresource3
    filePath: ${downloadcomponent2.spec.filePath}
    resource:
      name: helloeurope
  - type: uploader.localblob.ctf/v1alpha1
    id: uploadresource3
    filePath: ${constants.spec.attributes.targetFilePath}
    componentDescriptor: ${downloadcomponent2.outputs.descriptor}
    data: ${downloadresource3.outputs.data}

  - type: uploader.component.ctf/v1alpha1
    filePath: ${constants.spec.attributes.targetFilePath}
    componentDescriptor: ${uploadresource3.outputs.descriptor}
```

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
    resources). They are omitted here for brevity.
> * **Multiple Components:** The mappings property is a list. This allows
    transferring multiple components in one transfer operation based on a single
    transfer spec.

This specification contains all the information necessary to perform a transfer:

* Source and target location of the component
* Target location of the resources (and sources, if any)
* Transformations required to perform the upload to the target location such as:
  * format adjustments (e.g. local blob to oci artifact)
  * [localization](./0004_localization_at_transfer_time.md)

The transformation are implemented as plugins.

* The properties such as `imageReference` are passed to the plugin specified by
  the `type` as configuration.
* The byte stream of the resource content is passed from each transformation to
  the next transformation forming a pipeline.
* Besides the resource content, each transformation can also edit the resource
  specification in the component descriptor (e.g. adjust the digest after a
  format change or add a label)

> **NOTE:** The transformations are significantly more powerful than shown
> here. But this part should suffice to illustrate the concept for the basic
> transfer behavior.  
> For details about the transformation contract and implementation, refer to
> the localization adr [here](./0004_localization_at_transfer_time.md).

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
  [documentation](https://ocm.software/docs/cli-reference/help/ocm-uploadhandlers/)).
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
