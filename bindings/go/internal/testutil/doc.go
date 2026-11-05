// Package testutil holds test helpers shared by the bindings. Each concern lives in its
// own subpackage, so a test imports only the dependencies it needs:
//
//   - blobtest: read a [blob.ReadOnlyBlob] into memory
//   - logtest: capture [log/slog] records
//   - nexustest: requests to a Nexus server in integration tests
//   - pkitest: throwaway keys and certificates
//   - registrytest: OCI registries for integration tests
//   - s3test: an S3 server for integration tests
//   - tartest: gzipped tar archives
//   - typedtest: a [runtime.Typed] value no scheme knows
//
// The subpackages import only the standard library, test libraries, blob and runtime, so
// every binding's tests can use them without an import cycle. They are for tests only:
// a non-test file that imports them fails lint. depguard enforces both rules.
package testutil
