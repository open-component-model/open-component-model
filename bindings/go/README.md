# OCM Go Library

Go bindings for the Open Component Model.

## Packages

<!-- GENERATED PACKAGES TABLE:START (run 'task tools:dependency-list/generate' to update) -->

| Package | Purpose |
|---------|---------|
| **blob** | Provides various interfaces and types for working with Binary Large Objects (BLOBs) |
| **cel** | Provides CEL (Common Expression Language) support for OCM, including discovery, validation, and evaluation of expressions used in configurations |
| **cli** | Provides the command line interface for working with the Open Component Model |
| **configuration** | Provides generic and versioned configuration handling for OCM, including loading, merging, and scheme-based type registration |
| **constructor** | Provides functionality for creating and managing Open Component Model (OCM) component constructors through a flexible and extensible construction process |
| **credentials** | Provides a flexible and extensible credential management system for the Open Component Model (OCM) |
| **ctf** | Provides various interfaces and types for working with Common Transport Format Archives (CTF) |
| **dag** | Provides a directed acyclic graph implementation used to model and traverse OCM dependency graphs |
| **descriptor/normalisation** | Provides canonical normalisation of component descriptors for digest and signature computation |
| **descriptor/runtime** | Defines an internal runtime to work with component descriptors in all schema versions without restricting the code to the public API for future major changes |
| **descriptor/v2** | Defines the main objects that compose a component version descriptor |
| **generator** | Contains the code generators of the Go bindings, including ocmtypegen and jsonschemagen |
| **github** | Provides access to GitHub repositories as OCM resources and sources |
| **gpg** | Provides OpenPGP (GPG) signing and verification for OCM component descriptors |
| **helm** | Provides Helm chart handling for OCM, including chart repositories, constructor input, and resource access |
| **http** | Builds HTTP clients from the http.config.ocm.software configuration type |
| **input/dir** | Provides functionality for handling directory-based inputs in the Open Component Model (OCM) constructor |
| **input/file** | Provides functionality for handling file-based inputs in the Open Component Model (OCM) constructor |
| **input/utf8** | Provides functionality for handling UTF8 string-based inputs in the Open Component Model (OCM) constructor |
| **kubernetes/controller** | Provides the Kubernetes controllers that deploy OCM component versions into clusters |
| **oci** | Provides functionality for storing and retrieving Open Component Model (OCM) components using the Open Container Initiative (OCI) registry format |
| **plugin** | Provides the OCM plugin system for extending functionality through external plugin processes |
| **repository** | Provides an abstraction layer for working with different OCM (Open Component Model) repository technologies |
| **rsa** | Provides RSA key handling and RSA signing and verification for OCM component descriptors |
| **runtime** | Provides the core type system of the bindings, including typed objects, schemes, conversion, and JSON/YAML serialization |
| **s3** | Provides access to OCM resources stored as objects in an S3 or S3-compatible bucket |
| **signing** | Defines the interface for signing and verification of Component Descriptors |
| **sigstore** | Provides a signing handler for the Open Component Model that implements Sigstore-based keyless signing and verification by delegating to the cosign CLI tool |
| **transfer** | Provides functionality for transferring OCM component versions between repositories |
| **transform** | Provides transformation and localization of component versions when moving them between environments |
| **wget** | Provides HTTP(S) access to OCM resources, both as an access type and as a component-constructor input method |

<!-- GENERATED PACKAGES TABLE:END -->

## Usage

Import the packages you need:

```go
import (
    "ocm.software/open-component-model/bindings/go/oci"
    "ocm.software/open-component-model/bindings/go/descriptor/v2"
)
```

## Examples

The [`examples/`](examples/) directory contains runnable, tested examples for the most common OCM operations:

- **Blobs** — creating in-memory and filesystem blobs, copying with digest verification
- **Descriptors** — building component descriptors with resources, sources, references, and labels
- **Credentials** — resolving credentials by identity using the static resolver
- **Signing** — generating and verifying digests, RSA signing (plain and PEM), tamper detection
- **Repository** — creating CTF-backed repositories, storing and retrieving component versions, resources, and sources
- **OCI Registry** — full round-trip against a real OCI registry using testcontainers (skipped with `-short`)
- **Transfer** — transferring component versions between CTF repositories using the transfer graph API

All examples are self-contained and run as part of CI, except **OCI Registry** which requires a real OCI registry via
testcontainers and is skipped with `-short`:

```bash
cd bindings/go && go test -short ./examples/...
```

## Testing

```bash
# Run all library tests
task test
# Run specific tests
go test ./...
```

## Exploring tasks

```bash
# Run from repository root
task -a
```

## See Also

- [OCM Specification](https://github.com/open-component-model/ocm-spec)
- [CLI Documentation](cli/docs/reference/ocm.md)
- [Architecture Decisions](../../docs/adr/)
