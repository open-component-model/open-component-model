# A Guided Tour of the OCM Go Bindings

This module walks you through the core concepts of the Open Component Model,
starting from the most basic building block and progressively building up to
complete workflows.

Every example is a **runnable Go test** — read the code, run it, modify it.

## The Tour

| Step | File                                               | Topic                        | What You'll Learn                                                              |
|------|:---------------------------------------------------|------------------------------|--------------------------------------------------------------------------------|
| 1    | [`01_blob_test.go`](01_blob_test.go)               | **Binary Data (Blobs)**      | In-memory and filesystem blobs, metadata, copying with digest verification     |
| 2    | [`02_descriptor_test.go`](02_descriptor_test.go)   | **Component Descriptors**    | Building descriptors with resources, sources, references, labels, and identity |
| 3    | [`03_credentials_test.go`](03_credentials_test.go) | **Credential Resolution**    | Static credential resolvers, identity-based lookup                             |
| 4    | [`04_repository_test.go`](04_repository_test.go)   | **Repositories**             | CTF-backed storage, component version CRUD, resource upload and retrieval      |
| 5    | [`05_signing_test.go`](05_signing_test.go)         | **Signing & Verification**   | Digests, RSA signing (plain and PEM), tamper detection, wrong-key detection    |
| 6    | [`06_oci_test.go`](06_oci_test.go)                 | **OCI Registry Round-Trips** | Full push/pull workflow against a real OCI registry (requires Docker)          |
| 7    | [`07_transfer_test.go`](07_transfer_test.go)       | **Transferring Components**  | Building transfer graphs, CTF-to-CTF transfer, verifying transferred content   |

## How to Read

Start with Step 1 and work forward. Each file begins with a header explaining
what you'll learn, and each test function is a self-contained example with
comments explaining the "why" alongside the code.

## Running the Examples

```bash
# Run all examples (except OCI tests that need Docker)
cd bindings/go/examples && go test -short -v ./...

# Run all examples including OCI registry tests
cd bindings/go/examples && go test -v ./...

# Run a single example
cd bindings/go/examples && go test -run TestExample_InMemoryBlob -v

# Run via task
task bindings/go/examples:test
```

## Concept Map

```text
                    ┌─────────────┐
                    │  1. Blobs   │  Raw binary data
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │ 2. Descrip- │  Metadata model:
                    │    tors     │  resources, sources, refs
                    └──────┬──────┘
                           │
                ┌──────────┼──────────┐
                │          │          │
         ┌──────▼──────┐   │   ┌──────▼──────┐
         │3. Creden-   │   │   │ 5. Signing  │
         │   tials     │   │   │             │
         └──────┬──────┘   │   └──────┬──────┘
                │          │          │
                └──────────┼──────────┘
                           │
                    ┌──────▼──────┐
                    │ 4. Reposi-  │  Store & retrieve
                    │    tories   │  component versions
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │ 6. OCI      │  Real registry
                    │  Registries │  round-trips
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │ 7. Transfer │  Move components
                    │             │  between repos
                    └─────────────┘
```
