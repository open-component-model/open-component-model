// Package examples contains runnable usage examples for the OCM Go bindings.
//
// Each test function demonstrates a specific use case and serves as both
// documentation and a regression test. Examples are organized by topic:
//
//   - blob_test.go: Creating and reading binary data with the blob abstraction
//   - descriptor_test.go: Building component descriptors with resources, sources, and references
//   - credentials_test.go: Resolving credentials by identity
//   - signing_test.go: Generating and verifying component descriptor digests, RSA signing and verification
//   - repository_test.go: Storing and retrieving component versions in a CTF-backed OCI repository
//   - oci_test.go: Full OCI registry round-trips using testcontainers (skipped with -short)
//
// All examples are self-contained and use in-memory or temporary filesystem backends,
// so they run without external services.
package examples
