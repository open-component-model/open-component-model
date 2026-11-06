// Package testutil holds test helpers shared by the bindings. Each concern lives in its
// own subpackage, so a test imports only the dependencies it needs:
//
//   - blobtest: read a [blob.ReadOnlyBlob] into memory
//   - descriptortest: a [descriptor.Descriptor] with defaults
//   - githubtest: a fake GitHub API server
//   - helmtest: a fake Helm chart registry
//   - logtest: capture [log/slog] records
//   - nexustest: a Nexus server for integration tests
//   - pkitest: throwaway keys and certificates
//   - registrytest: OCI registries for integration tests
//   - s3test: an S3 server for integration tests and a fake for unit tests
//   - tartest: gzipped tar archives
//   - tsatest: a mock RFC 3161 timestamp authority
//   - typedtest: a [runtime.Typed] value no scheme knows
//
// The subpackages import only the standard library, test libraries, blob, runtime and
// descriptor/runtime, so every binding's tests can use them without an import cycle.
// They are for tests only: a non-test file that imports them fails lint. depguard
// enforces both rules.
package testutil
