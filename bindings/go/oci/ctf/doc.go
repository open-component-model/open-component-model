// Package ctf implements oras OCI interfaces on the Common Transport Format
// (CTF). It can hold a selection of repositories and artifacts that can be
// imported back into any OCI registry. It implements the referrer tag schema.
//
// This package also exposes a legacy compatibility layer for ArtifactSet, a
// deprecated format previously used by the OCM CLI to package local blobs.
package ctf
