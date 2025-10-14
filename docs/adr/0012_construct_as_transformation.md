# A OCM Transformation Specification

* **Status**: proposed
* **Deciders**: Gergely Brautigam, Fabian Burth, Jakob Moeller
* **Date**: 2025-10-14

**Technical Story:** Unify the technical foundation of 
[transformation](0005_transformation.md) and 
[component constructors](0006_component_constructors.md).


## Context and Problem Statement

Initially, the transformation specification was primarily designed for the 
requirements of **transferring of components and their resources**. However, 
we noticed a big overlap with the requirements of **component constructors**.

### Requirements

| Requirement                    | Transfer                                                                                      | Constructor                                                                                               |
|--------------------------------|-----------------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------|
| Upload of Component Versions   | Upload component versions downloaded from a source repository to a target repository          | Upload constructed component versions to a target repository                                              |
| Download of Component Versions | Download component version from a source repository to be uploaded to a target repository     | Download referenced component versions to be uploaded to a target repository (`--recursive`)              |
| Download resources             | Download resources based on access type specifications                                        | Download resources based on access and input type specifications                                          |
| Upload resources               | Upload resources based on uploader specification                                              | Upload resources based on access and input type specifications                                            |
| Resource format conversion     | Convert resources downloaded based on one access type to be uploaded with another access type | Convert resources based on the requirements of the input type (e.g. helm input type allows upload to oci) |
| Topological graph processing   | Topological order required for hashing, signing and localization                              | Topological order required for hashing                                                                    |
| Parallelization of operations  |                                                                                               |                                                                                                           |

### Conclusion

The functional requirements of both use cases are almost identical.

The difference between the use cases is currently the **interface**:
- for **ocm transfer component**, the plan is to offer a backwards compatible 
  high-level command (`ocm transfer component`) that generates the 
  transformation specification based on the provided parameters.
- for **ocm add component** (constructor), the established user interface is the
  constructor file.

## Solution Proposal

To keep the current user experience and enable an easy transition to ocm v2, we 
propose to ALSO generate a transformation specification based on the constructor
file.

### Example: OCM Transformation Specification

Assume we call `ocm add cv --repository ghcr.io/fabianburth/target-ocm-repository` 
with the following constructor file:

```yaml
components:
  - name: github.com/acme.org/helloworld
    version: 1.0.0
    provider:
      name: internal
    resources:
      - name: testdata
        type: blob
        relation: local
        input:
          type: file
          path: ./testdata/text.txt
```

We can generate the following transformation specification:

#### Option 1)

```yaml
type: transformations.ocm/v1alpha1
transformations:
  # component
  - type: create.componentversion/v1alpha1
    id: createcomponentversion1
    name: github.com/acme.org/helloworld
    version: 1.0.0
    provider:
      name: internal
      
  # resource
  - type: create.resource/v1alpha1 # maps to a resource in a constructor file
    id: createresource1
    resource: # maps to the resource specification in a constructor file
      name: testdata
      type: blob
      relation: local
  - type: downloader.blob.filesystem/v1alpha1 # maps to input type "file"
    id: downloadblob1
    filePath: ./testdata/text.txt # maps to input.path
  - type: uploader.localblob.oci/v1alpha1 # maps to the access or input type
    id: uploadresource1
    # maps to the target repository (for local blobs) or an input or access
    # specification property
    imageReference: ghcr.io/fabianburth/target-ocm-repository/github.com/acme.org/helloworld:1.0.0
    resource: ${createresource1.outputs.spec}
    data: ${downloadresource1.outputs.data}

  # component
  - type: merge.component/v1alpha1
    id: merge
    merge:
      base: ${createcomponentversion1.outputs.descriptor} # maps to the created component version
      patches: # maps to each resource in the constructor file
        - ${uploadresource1.outputs.spec}
  - type: uploader.component.oci/v1alpha1
    imageReference: ${uploadresource1.imageReference} # maps to the target repository
    componentDescriptor: ${merge.outputs.descriptor}
```

Compared to the transformation specification for transferring a component, this:
- Replaces the download of the component with the creation of a new 
  component version specification.
  So, creating a component version from scratch can essentially be thought of as 
  a special kind of download of a component version.
- Replaces the download of blob data based on an ocm resource specification with
  a download based on a particular download specification (in this case, a 
  file path) AND the creation of a new resource specification.

#### Option 2

```yaml
type: transformations.ocm/v1alpha1
transformations:
  # component
  - type: create.componentversion/v1alpha1
    id: createcomponentversion1
    name: github.com/acme.org/helloworld
    version: 1.0.0
    provider:
      name: internal
    resources:
    - name: testdata
      type: blob
      relation: local
      input:
        type: file
        path: ./testdata/text.txt
        
  # resource
  - type: downloader.resource.file/v1alpha1
    id: downloadresource1
    componentDescriptor: ${createcomponentversion1.outputs.descriptor}
    resource:
      name: testdata
  - id: uploadresource1
    imageReference: ghcr.io/fabianburth/target-ocm-repository/github.com/acme.org/helloworld:1.0.0
    resource: ${downloadresource1.outputs.spec}
    data: ${downloadresource1.outputs.data}

  # component
  - type: merge.component/v1alpha1
    id: merge
    merge:
      base: ${createcomponentversion1.outputs.descriptor}
      patches:
        - ${uploadresource1.outputs.descriptor}
  - type: uploader.component.oci/v1alpha1
    imageReference: ${uploadresource1.imageReference}
    componentDescriptor: ${merge.outputs.descriptor}
```

Compared to option 1), this even strengthens idea of the component creation 
being a special kind of download. Here, the following transformations are also
equivalent to transfer and we do not require a separate resource creation 
transformation.

### Comparison of Options
**Option 1)** 
- offers a clearer separation of concerns. The creation of the component
and resource specifications is clearly separated from the download and upload
of data.
- is more flexible, as it allows for adding new resource to existing component
versions.
- is more complex, as it requires more transformations.

**Option 2)**
- is simpler, as it requires fewer transformations.
- is closer to the current implementation of constructors.
- to unify the code path, the transformations would have to be able to deal 
  with a component constructor that allows input types (or some other concept 
  of access types that are not allowed to be present in a descriptor that is
  uploaded).

## Conclusion


## Links <!-- optional -->
