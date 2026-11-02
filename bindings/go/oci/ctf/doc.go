// Package ctf implements oras OCI interfaces on the Common Transport Format
// (CTF). It can hold a selection of repositories and artifacts that can be
// imported back into any OCI registry. It implements the referrer tag schema.
//
// Legacy artifact sets embedded as local blobs are read by the oci/tar package
// (see ReadOCILayout), not by this package.
package ctf
