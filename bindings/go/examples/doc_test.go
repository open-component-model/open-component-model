// Package examples is a guided tour of the OCM Go bindings.
//
// It walks you through the core concepts of the Open Component Model, starting
// from the most basic building block (binary data) and progressively building
// up to complete workflows involving repositories, signing, and OCI registries.
//
// The tour is organized as numbered test files that should be read in order:
//
//  1. [01_blob_test.go] — Binary data: creating, reading, and copying blobs
//  2. [02_descriptor_test.go] — Component descriptors: the metadata model
//  3. [03_credentials_test.go] — Credential resolution by identity
//  4. [04_repository_test.go] — Storing and retrieving component versions
//  5. [05_signing_test.go] — Digests, RSA signing, and verification
//  6. [06_oci_test.go] — Full OCI registry round-trips (requires Docker)
//
// Every example is a runnable test. Run them all with:
//
//	task bindings/go/examples:test
//
// Or run a single step:
//
//	cd bindings/go/examples && go test -run TestExample_InMemoryBlob -v
//
// All examples (except Step 6) are self-contained and use in-memory or
// temporary filesystem backends, so they run without external services.
package examples
