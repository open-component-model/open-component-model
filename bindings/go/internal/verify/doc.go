// Package verify compares downloaded content to the digest a component descriptor
// declares for it. It is internal to bindings/go and not part of the public API.
//
// Download wraps a blob so that reading it hashes the content and compares. It is
// only usable where the download returns exactly the bytes the digest represents,
// which rules out a repository whose digest covers something else, such as an OCI
// image resource whose digest is that of its manifest.
package verify
