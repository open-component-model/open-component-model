// Package repository provides an abstraction layer for working
// with different OCM (Open Component Model) repository technologies.
//
// By programming against these technology-agnostic repository interfaces,
// consumers of the module can seamlessly switch between
// repository backends without changing their application logic.
//
// Key features:
//   - Defines core interfaces for component version repositories, including
//     methods for adding, retrieving, and listing component versions.
//   - Supports extensibility for new repository technologies by allowing them to
//     implement the provided interfaces.
//   - Facilitates integration with the broader OCM ecosystem by standardizing
//     repository interactions.
//   - Implements higher abstraction layer functionality that does not care about
//     or depend on the underlying repository technology, but only on the
//     interfaces defined in this package (such as the fallback repository).
//
// Use this module when you need to work with OCM repositories in a generic way,
// or when building new repository backends that should be compatible with the
// OCM tooling and workflows.
//
// # Content and contexts
//
// Blobs returned by GetLocalResource, GetLocalSource, DownloadResource and
// DownloadSource may be read lazily, and their reads may be bound to the context of
// the call that returned them: once that context is done, reading fails. Read such a
// blob while that context lasts; to read it later, fetch it with a context that
// lasts as long, or fetch it where it is read.
package repository
