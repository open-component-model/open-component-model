# Next Generation Component Constructor Support

* Status: proposed
* Deciders: Gergely Brautigam, Fabian Burth, Jakob Moeller
* Date: 2025.05.15

Technical Story: Enable easy and simple construction of component versions based on a `component-constructor.yaml` specification that is backwards compatible and integrates seamlessly with the new OCM Plugin System.

## Context and Problem Statement

Whenever users of OCM want to build component versions, it is important to understand that the existing binding libraries offer support to create component versions with API Calls.

However, when working within the context of a CLI, the user experience is not as straightforward. There is a need for a more user-friendly approach to constructing component versions, especially for those who may not be familiar with the underlying API calls in the Go API directly.

As such we want to continue supporting commands in the form of

```shell
ocm add componentversions ./ctf component-constructor.yaml
```

This will require the support of the component constructor schema.
Currently the schema is available on the ocm website as a [statically supported JSON scheme](https://ocm.software/schemas/configuration-schema.yaml)
This schema is used to validate the component constructor YAML file and ensure that it adheres to the expected structure and data types.

The idea is to provide a similar experience as the existing OCM CLI that focuses on

- maintaining the format and structure of the `component-constructor.yaml` file schema
- ensuring that input methods can be registered dynamically (via the plugin manager) or inbuilt (via the OCM CLI)
- reusing the existing binding libraries to interact with OCM repositories

## Decision Drivers
    
* Simplicity of Architecture: While component constructors are central to our user flow, they are also an integral
  part of our development experience. As such, writing input methods must be simple and easy to adopt for even
  less experienced engineers.
* Extensibility: The component constructor schema must stay flexible enough to allow for the addition of new input methods
  and the ability to extend existing ones.
* Maintainability: The constructor logic and input methods should stay separated from existing OCI libraries and
  binding libraries. This will allow us to maintain the component constructor schema and its input methods without
  affecting the existing libraries.

## Outcome

We choose to implement a new component constructor library as described due to the following reasons:

- The existing component constructor library is not extensible enough to allow for the addition of new input methods
  and the ability to extend existing ones.
- The existing component constructor library is not simple enough to be adopted by other engineers in acceptable timeframes.

## A simplified `bindings/go/constructor` library

The core idea of this proposal is the introduction of a new module `bindings/go/constructor` that will be responsible for

* Providing centralized access and interaction with `component-constructor.yaml` style files and their schemas
* Providing centralized extension points for adding new Input Methods for building component versions in addition
  to directly referencing an access.

## Input Methods and Contracts

A key feature of the `component-constructor.yaml` file is the ability to define **inputs**—specifications that describe where to retrieve data and how to store it in a resource for a newly created component version.

### Purpose of Input Methods

An **input method** replaces the `access` field in a standard OCM resource or source. When specified, the input is interpreted at runtime during component construction and used to generate an appropriate access specification based on the processed input data.

### Example

The following `component-constructor.yaml` defines a simple input method:

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

which could be processed by a constructor library as follows:

```yaml
component:
  componentReferences: []
  name: github.com/acme.org/helloworld
  provider: internal
  repositoryContexts: []
  resources:
  - access:
      localReference: sha256:c3ab8ff13720e8ad9047dd39466b3c8974e592c2fa383d4a3960714caef0c4f2
      mediaType: application/octet-stream
      type: localBlob/v1
    creationTime: "2025-05-19T08:28:30+02:00"
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: genericBlobDigest/v1
      value: c3ab8ff13720e8ad9047dd39466b3c8974e592c2fa383d4a3960714caef0c4f2
    name: testdata
    relation: local
    size: 6
    type: blob
    version: 1.0.0
  sources: []
  version: 1.0.0
meta:
  schemaVersion: v2

``` 

In this example:

- The input of type `file` tells the constructor to use the registered `file` input method.
- This method processes `./testdata/text.txt` and generates an access specification.
- The result is included in the final component descriptor.
- The format of the generated access specification depends on the specific input method used.

---

### Categories of Input Methods

Input methods are typically categorized into two types:

#### Local Input Methods

- Store the content as a `localBlob` next to the generated component version.

#### Global Input Methods

- Generate access specifications that are independent of the constructed component version.

Each input method must determine whether it should produce a `localBlob` or a global access specification.

---

### "By Value" Construction

Any input or access specification can be marked as **constructed by value**. In this case:

- The constructor directly embeds the resource into the component version.
- The resource is uploaded to the OCM repository as a co-located `localBlob`.

To support this behavior, the constructor must be capable of:

- Uploading the constructed component version.
- Downloading input data as needed.
- Uploading resources again as local blobs for inclusion.

### Automatically discovering digests for resources added by reference

In old OCM versions, adding a resource with an access automatically downloaded the resource and
calculated digest information for it based on the downloaded data.

For this reason, it should be possible for accesses to be enriched with a digest.

For example, the resource

```yaml
access:
  localReference: sha256:c3ab8ff13720e8ad9047dd39466b3c8974e592c2fa383d4a3960714caef0c4f2
  mediaType: application/octet-stream
  type: localBlob/v1
name: testdata
relation: local
type: blob
version: 1.0.0
```

should be extendable by a plugin to contain a digest such as

```yaml
digest:
  hashAlgorithm: SHA-256
  normalisationAlgorithm: genericBlobDigest/v1
  value: c3ab8ff13720e8ad9047dd39466b3c8974e592c2fa383d4a3960714caef0c4f2
```

This is important, because unlike the access, a digest is included in component version signatures, while an access is not.

As such it is imperative that component versions constructed eventually also have the digest information set.

To achieve this, the constructor library will

- Look for any resource added "by reference" and check if the digest is NOT set.
- If there are any, the constructor library will attempt to lookup a digest provider
  for the resource type.
- If a digest provider is found, it will be called with the resource and the constructor library
  will set the digest information on the resource based on the provided function.

_Note: For input types, this is less of an issue, because while processing the input, the digest can be provided
by the input method._

## Processing Architecture

All `component-constructors` follow a simple and consistent processing pattern:

1. **Define Specification**   
   The user specifies the desired component version in a `component-constructor.yaml` file, typically via the CLI.

2. **Parse Component Versions**  
   The constructor library parses the specified component versions from the YAML file.

3. **Process Resources and Sources**  
   For each component version, the constructor handles all resources and sources:

    - **Input Method Specified**  
      If an input method is defined for resources:
        - If it returns `ResourceInputMethodResult.ProcessedBlobData`, the constructor uploads the local blob to the target OCM repository.
          (if this is the case, the resource is automatically marked as `by value`)
        - If it returns `ResourceInputMethodResult.ProcessedResource`, the constructor applies the resource directly to the component descriptor candidate.
          (if this is the case, the resource is automatically marked as `by reference`)
      _Note: The input method must be registered in the constructor library._
      _Sources are processed in the same way with `SourceInputMethodResult._

    - **Access Specified**  
      If an access method is defined, it is applied directly to the component descriptor candidate. However, it can
      be explicitly interpreted as `by value` or `by reference`.

    - **"By Value" Resources / Sources**  
      If the resource is marked to be processed "by value":
        - The constructor downloads the resource (if not already available as a `localBlob`).
        - It is then stored in the component version as a `localBlob`.
        - The local blob is uploaded using the OCM repository's capabilities.
    
    - **"By Reference" Resources**
      If the resource is marked to be processed "by reference":
        - The constructor checks if the resource has a digest set.
        - If not, it attempts to find a digest provider for the resource type.
        - If found, the digest provider is called with the resource, and the digest information is set on the resource.
      _Note: Sources do not have digest information and will not get processed like this._

4. **Upload Component Version**  
   Once all resources, sources, and metadata are processed, the constructor uploads the final component version to the OCM repository.

### Contract

From this set of requirements we can derive the following interface for input methods:

```go
// ResourceInputMethodResult is the return value of a ResourceInputMethod.
// It MUST contain one of
// - ProcessedResource: The processed resource with the access type set to the resulting access type
// - ProcessedBlobData: The local blob data that is expected to be uploaded when uploading the component version
//
// If the input method does not support the given input specification it MAY reject the request,
// but if a ResourceInputMethodResult is returned, it MUST at least contain one of the above.
//
// If the ResourceInputMethodResult.ProcessedResource is set, the access type of the resource MUST be set to the resulting access type
// If the ResourceInputMethodResult.ProcessedBlobData is set, the access type of the blob must be uploaded as a local resource
// with the relation `local`, the media type derived from blob.MediaTypeAware, and the resource version defaulted
// to the component version.
type ResourceInputMethodResult struct {
    ProcessedResource *descriptor.Resource
    ProcessedBlobData blob.ReadOnlyBlob
}

// ResourceInputMethod is the interface for processing a resource with an input method declared as per
// [spec.Resource.Input]. Note that spec.Resource's who have their access predefined, are never processed
// with a ResourceInputMethod, but are directly added to the component version repository.
// any spec.Resource passed MUST have its [spec.Resource.Input] field set.
// If the input method does not support the given input specification it MAY reject the request
//
// The method will get called with the raw specification specified in the constructor and is expected
// to return a ResourceInputMethodResult or an error.
//
// A method can be supplied with credentials from any credentials system by requesting a consumer identity with
// ResourceInputMethod.GetCredentialConsumerIdentity. The resulting identity MAY be used to uniquely identify the consuming
// method and to request credentials from the credentials system. The credentials system is not part of this interface and
// is expected to be supplied by the caller of the input method.
//
// The resolved credentials MAY be passed to the input method via the credentials map, but a method MAY
// work without credentials as well.
type ResourceInputMethod interface {
    GetCredentialConsumerIdentity(ctx context.Context, resource *constructor.Resource) (identity runtime.Identity, err error)
    ProcessResource(ctx context.Context, resource *constructor.Resource, credentials map[string]string) (result *ResourceInputMethodResult, err error)
}



// SourceInputMethodResult is the return value of a SourceInputMethod.
// It MUST contain one of
// - ProcessedSource: The processed source with the access type set to the resulting access type
// - ProcessedBlobData: The local blob data that is expected to be uploaded when uploading the component version
//
// If the input method does not support the given input specification it MAY reject the request,
// but if a SourceInputMethodResult is returned, it MUST at least contain one of the above.
//
// If the ProcessedSource.ProcessedSource is set, the access type of the source MUST be set to the resulting access type
// If the ProcessedSource.ProcessedBlobData is set, the access type of the blob must be uploaded as a local resource
// with the relation `local`, the media type derived from blob.MediaTypeAware, and the resource version defaulted
// to the component version.
type SourceInputMethodResult struct {
    ProcessedSource *descriptor.Source
    ProcessedBlobData blob.ReadOnlyBlob
}


// SourceInputMethod is the interface for processing a source with an input method declared as per
// [spec.Source.Input]. Note that spec.Source's who have their access predefined, are never processed
// with a ResourceInputMethod, but are directly added to the component version repository.
// any spec.Source passed MUST have its [spec.Source.Input] field set.
// If the input method does not support the given input specification it MAY reject the request
//
// The method will get called with the raw specification specified in the constructor and is expected
// to return a SourceInputMethodResult or an error.
//
// A method can be supplied with credentials from any credentials system by requesting a consumer identity with
// SourceInputMethod.GetCredentialConsumerIdentity. The resulting identity MAY be used to uniquely identify the consuming
// method and to request credentials from the credentials system. The credentials system is not part of this interface and
// is expected to be supplied by the caller of the input method.
//
// The resolved credentials MAY be passed to the input method via the credentials map, but a method MAY
// work without credentials as well.
type SourceInputMethod interface {
    GetCredentialConsumerIdentity(ctx context.Context, source *constructor.Source) (identity runtime.Identity, err error)
    ProcessSource(ctx context.Context, source *constructor.Source, credentials map[string]string) (result *SourceInputMethodResult, err error)
}
```

The contract explicitly opens the possibility to integrate our existing systems such as our credential graph.
However, because methods must be available at runtime to the constructor library, we also support a set of provider
interfaces that can be integrated with our plugin system.

```go
type ResourceInputMethodProvider interface {
    // GetResourceInputMethod returns the input method for the given resource constructor specification.
    GetResourceInputMethod(ctx context.Context, resource *constructor.Resource) (input.ResourceInputMethod, error)
}

type SourceInputMethodProvider interface {
    // GetSourceInputMethod returns the input method for the given source constructor specification.
    GetSourceInputMethod(ctx context.Context, resource *constructor.Resource) (input.ResourceInputMethod, error)
}
```

To allow the dynamic expansion of digest information for resources added by reference, we also provide a digest provider interface:

```go
type ResourceDigestProcessor interface {
    // ProcessResourceDigest processes the given resource and returns a new resource with the digest information set.
    // The resource returned MUST have its digest information filled appropriately or the method MUST return an error.
    // The resource passed MUST have an access set that can be used to interpret the resource and provide the digest.
    ProcessResourceDigest(ctx context.Context, resource *descriptor.Resource) (*descriptor.Resource, err error)
}

type ResourceDigestProcessorProvider interface {
    // GetDigestProcessor returns the digest processor for the given resource constructor specification.
    GetDigestProcessor(ctx context.Context, resource *descriptor.Resource) (DigestProcessor, error)
}
```

To allow for actual construction of component versions, the constructor library provides a central entry function:

```go
// TargetRepository defines the interface for a target repository that can store component versions and associated local resources
type TargetRepository interface {
    // AddLocalResource adds a local resource to the repository.
    // The resource must be referenced in the component descriptor.
    // Resources for non-existent component versions may be stored but may be removed during garbage collection cycles
    // after a time set by the underlying repository implementation.
    // Thus it is mandatory to add a component version to permanently persist a resource added with AddLocalResource.
    // The Resource given is identified later on by its own Identity and a collection of a set of reserved identity values
    // that can have a special meaning.
    AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (newRes *descriptor.Resource, err error)
    // AddComponentVersion adds a new component version to the repository.
    // If a component version already exists, it will be updated with the new descriptor.
    // The descriptor internally will be serialized via the runtime package.
    // The descriptor MUST have its target Name and Version already set as they are used to identify the target
    // Location in the Store.
    AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error
}


type TargetRepositoryProvider interface {
    // GetTargetRepository returns the target ocm component version repository 
    // for the given component specification in the constructor.
    GetTargetRepository(ctx context.Context, *constructor.Component) (TargetRepository, error)
}

type ResourceRepository interface {
    // DownloadResource downloads a resource from the repository.
    DownloadResource(ctx context.Context, res *descriptor.Resource) (content blob.ReadOnlyBlob, err error)
}

type ResourceRepositoryProvider interface {
    // ResourceRepositoryProvider returns the target ocm resource repository for the given component specification in the constructor.
    GetResourceRepository(ctx context.Context, *constructor.Component) (Repository, error)
}

// Options are the options for construction based on a *constructor.Constructor.
type Options struct {
    // While constructing a component version, the constructor library will use the given target repository provider
    // to get the target repository for the component specification.
    TargetRepositoryProvider
    
    // While constructing a component version, the constructor library will use the given resource repository provider
    // to get the resource repository for the component specification when processing resources by value.
    ResourceRepositoryProvider
    
    // While constructing a component version, the constructor library will use the given resource input method provider
    // to get the resource input method for the component specification when processing resources with an input method.
    ResourceInputMethodProvider
    
    // While constructing a component version, the constructor library will use the given source input method provider
    // to get the source input method for the component specification when processing sources with an input method.
    SourceInputMethodProvider
    
    // While constructing a component version, the constructor library will use the given digest processor provider
    // to get the digest processor for the component specification when processing resources by reference to ammend
    // digest information.
    ResourceDigestProcessorProvider

    // While constructing a component version, the constructor library will use the 
    // given function to decide whether a resource should be processed by value or not.
    ProcessByValue func(*constructor.Resource) bool
}

// Construct constructs a component version based on the given specification and options.
func Construct(ctx context.Context, specification *constructor.Constructor, opts Options) ([]*descriptor.Descriptor, error)
```

#### The Constructor Library / Specification

To enable working with constructor files, there must be a serialization data structure present that can be used by
the contracts above. This is parallel to our component descriptor binding libraries.

This separate specification is made available under a [separate PR](https://github.com/open-component-model/open-component-model/pull/111) due to its size.

## Pros and Cons

Pros:

* Very modular and encapsulated constructor design
* Extensible and easy to add new input methods by adjusting the providers
* Very easy control flow that can be made concurrent

Cons:

* Complex Interfaces for a lot of different scenarios (input and access specs are mutually exclusive and lead to branching)
* By default, accesses by reference do not get their digest information added when pushed into the constructor.

## Environment Variable Substitution

Component constructor files support environment variable substitution to enable dynamic configuration at construction time. This is implemented using Go's `os.Expand()` function, which performs variable expansion using the shell syntax `${VAR_NAME}` or `$VAR_NAME`.

### Processing Order

1. **File Loading**: The component constructor YAML file is read from disk
2. **Environment Expansion**: All environment variables in the file content are expanded using `os.Expand(string(constructorData), os.Getenv)`
3. **YAML Parsing**: The expanded content is then parsed as YAML into the component constructor specification
4. **Component Construction**: The constructor processes the expanded specification normally

### Supported Syntax

The environment variable substitution follows standard shell variable expansion:

- `${VAR_NAME}` - expands to the value of VAR_NAME
- `$VAR_NAME` - expands to the value of VAR_NAME

### Use Cases

Common use cases for environment variable substitution in component constructors include:

- **Dynamic Versioning**: Using CI/CD pipeline variables for component versions
- **Registry Configuration**: Configuring different registries per environment (dev/staging/prod)
- **Credential References**: Referencing credential paths or identifiers
- **Build-time Configuration**: Injecting build metadata or configuration values

### Security Considerations

- Environment variables are expanded at file processing time, before any validation
- All environment variables accessible to the process are available for substitution
- Care should be taken to avoid exposing sensitive information in constructor files
- Constructor files with environment variables should be treated as templates

## Conclusion

The new component constructor library will:

- Provide a simple and extensible way to construct component versions based on a `component-constructor.yaml` specification.
- Support environment variable substitution for dynamic configuration.
- Support various input methods and access specifications, and upload the resulting component version to an OCM repository.
- Download resources and upload them as local blobs.
- Process component version resources and sources in parallel to improve efficiency.
- Offer a modular architecture that makes it easy to extend with new input methods and access types.
