// Package ctf implements the Common Transport Format (CTF): a filesystem
// layout that realizes a subset of oras OCI interfaces. It can hold a
// selection of repositories and artifacts that can be imported back into
// any OCI registry. It implements the referrer tag schema.
//
// Layout:
//
//   - artifact-index.json — index of contained repositories and artifacts (OCI
//     image and index manifests).
//   - blobs/ — flat directory of blobs referenced by the index. Each
//     filename is the blob digest with the algorithm separator ":" replaced
//     by ".". Blobs not referenced by the index SHOULD be ignored.
//
// A CTF may be stored as a directory (FormatDirectory), an uncompressed TAR
// (FormatTAR), or a gzipped TAR (FormatTGZ). In archive form, the index
// SHOULD be the first entry.
//
// This package also exposes a legacy compatibility layer for ArtifactSet, a
// deprecated format previously used by the OCM CLI to package local blobs.
// New code should use OCI Image Layouts instead; see ArtifactSet for
// details.
package ctf
