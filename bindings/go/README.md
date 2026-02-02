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
