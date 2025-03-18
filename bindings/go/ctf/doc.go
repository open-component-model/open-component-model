// Package ctf provides various interfaces and types for working with Common Transport Format Archives (CTF)
//
// The Common Transport Format describes a file system structure that can be
// used for the representation of [content](https://github.com/opencontainers/image-spec)
// of an OCI repository.
//
// Therefore, it can be used to describe a subset of repositories of an OCI registry with a subset of
// artifacts, that can then be imported again into any OCI registry.
//
// # A CTF is a file or directory containing
//
//   - artifact-index.json: This JSON file describes the contained artifact (versions).
//
//   - blobs
//
//     The blobs directory contains the blobs described by the
//     artifact index as a flat file list. These are layer blobs or artifact
//     blobs for the artifact descriptors.
//
//     Every file has a filename according
//     to its digest (https://github.com/opencontainers/image-spec/blob/main/descriptor.md#digests).
//     Hereby the algorithm separator character is replaced by a dot (".").
//
//     Every file SHOULD be referenced, directly or indirectly, in the artifact
//     descriptor by a descriptor according the OCI Image Specification (https://github.com/opencontainers/image-spec/blob/main/descriptor.md).
//
//     The artifact index describes the OCI manifests (image manifests and index
//     manifests), which refer to further non-manifest blobs.
//     Files not referenced by the artifacts described by the index SHOULD be ignored.
//
// The FileFormat of a CTF can differ: as directory of an
// operating system file system or a virtual file system (FormatDirectory) or as content of
// a TAR archive (unzipped - FormatTAR or zipped - FormatTGZ).
// The descriptor SHOULD be the first file if stored in an archive.
package ctf
