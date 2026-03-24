# OCM Go Library

Go bindings for the Open Component Model.

## Modules

| Module                       | Purpose                                                  |
|------------------------------|----------------------------------------------------------|
| **runtime**                  | Core type system, JSON/YAML encoding, type registration  |
| **dag**                      | Directed acyclic graph utilities for dependency handling |
| **cel**                      | CEL (Common Expression Language) evaluation              |
| **blob**                     | Blob abstraction for artifact content                    |
| **configuration**            | Configuration management and merging                     |
| **credentials**              | Credential resolution and storage                        |
| **descriptor/v2**            | OCM v2 component descriptor schema                       |
| **descriptor/runtime**       | Runtime descriptor type handling                         |
| **descriptor/normalisation** | Canonical descriptor format for signing                  |
| **signing**                  | Component version signing and verification               |
| **rsa**                      | RSA key handling                                         |
| **repository**               | Abstract repository interface                            |
| **ctf**                      | Common Transport Format (filesystem-based)               |
| **oci**                      | OCI registry repository implementation                   |
| **constructor**              | Build and assemble component versions                    |
| **plugin**                   | Plugin system for extensibility                          |
| **transform**                | Transformation/localization of component versions        |
| **helm**                     | Helm chart resource handling                             |
| **input**                    | Input sources (file, directory, utf8)                    |
| **generator**                | Code generation tools                                    |

## Usage

Import the modules you need:

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

All examples are self-contained (no external services required) and run as part of CI:

```bash
task bindings/go/examples:test
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
- [CLI Documentation](../../cli/docs/reference/ocm.md)
- [Architecture Decisions](../../docs/adr/)
