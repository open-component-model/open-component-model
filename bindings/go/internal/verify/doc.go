// Package verify compares downloaded content to the digest a component descriptor
// declares for it. It is internal to bindings/go and not part of the public API.
//
// Two functionalities live here:
//   - Download wraps a blob so that reading it hashes the content and compares.
//     Use it where the download returns exactly the bytes the digest represents.
//   - Digest compares a digest the repository already resolved. Use it where the
//     digest covers something else, such as an OCI image resource whose digest is
//     that of its manifest.
package verify
