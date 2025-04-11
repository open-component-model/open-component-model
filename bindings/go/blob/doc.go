// Package blob provides various interfaces and types for working with Binary Large Object's (BLOBs).
//
// When working with BLOBs through this package, it is important to understand the following concepts:
//   - ReadOnlyBlob: An interface that represents a read-only BLOB.
//   - Blob: An interface that represents a BLOB that can be both read and written to.
//   - SizeAware: An interface that represents any arbitrary object that can be sized.
//   - DigestAware: An interface that represents any arbitrary object that can be digested.
//   - MediaTypeAware: An interface that represents any arbitrary object that can have a media type.
//
// These core concepts make it possible to describe any arbitrary data without actually introspecting it.
// The Blob is the fundamental interface that can be used by implementations to provide abstractions over
// dynamic content-ware, size-aware and content-proof data.
//
// Additionally, the package provides convenience implementations of typical blob scenarios:
//   - EagerBufferedReader: A reader that eagerly buffers the data it reads, to make it easier to
//     introspect the data. This is useful for cases where the size and digest of the data are not known
//     in advance but still need to be computed (through buffering in-memory instead of directly streaming).
//   - Copy: A function that copies data from a blob to any given io.Writer, while respecting SizeAware and
//     DigestAware for open-container type digests.
//
// Note that filesystem-backed blobs are located in a separated sub-package.
package blob
