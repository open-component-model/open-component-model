// Package resource implements a repository.ResourceRepository for PyPI
// distribution files addressed by the pypi/v1alpha1 access type.
//
// # Scope
//
// The access spec names a project and a version and, optionally, the subset of
// distribution files it wants (by kind — sdist/wheel — or by exact file name).
// Files are discovered from the Simple Repository API project detail page
// (<indexUrl>/<normalized-project>/): the PEP 691 JSON serialization is
// requested first, with the PEP 503 HTML serialization as a fallback. Every
// listed file carries its own download URL and, from the index, its hashes.
//
// Each selected file is fetched together with its detached PGP signature
// (".asc") when the index advertises one; the signature is stored exactly as
// served and never verified. A yanked file (PEP 592) is skipped unless it is
// named explicitly.
//
// Download always returns one application/x-tgz archive, even for a single
// file, with entries in a deterministic order and each file followed by its
// signature. One shape for every download means consumers never branch on the
// entry count. The archive is built with stored gzip blocks and fixed tar
// headers, so its bytes depend only on the files fetched, not on the Go
// release that built it. A PyPI release version is immutable, so the archive
// is stable over time (see PyPI.IsPinnedVersion).
//
// Upload is the inverse of download. It takes the same application/x-tgz,
// checks that it holds exactly the files the spec resolves to plus their
// optional signatures, and writes every entry unchanged under the project path
// (<indexUrl>/<normalized-project>/<filename>). No hashes are computed. Entries
// are written one by one and a failing PUT leaves the earlier ones deployed.
// This is what a by-value transfer needs to push a PyPI resource into a
// writable index (Nexus/Artifactory PyPI-hosted repositories accept a
// file-addressed PUT). Out of scope: publishing to upload.pypi.org, which uses
// a twine/PEP 694 multipart POST to a separate endpoint rather than a file PUT.
//
// # Credentials
//
// Credentials are resolved through the "PyPIRepository" consumer identity built
// from indexUrl and decoded once at the edge into PyPICredentials/v1
// (spec/credentials/v1); a DirectCredentials property bag is accepted too,
// including old OCM's "accessToken" key. An identityToken yields Bearer auth;
// username and password yield Basic auth (an API token is passed as the
// "__token__" username with the token as the password). Nil credentials, and a
// property bag without any PyPI key, mean an anonymous request. Credentials
// that carry a password but neither a token nor a username are an error, not a
// silent anonymous request.
//
// # Registration
//
// The CLI registers this repository together with the PyPI digest processor and
// the PyPICredentials/v1 credential scheme in its builtin plugin set.
package resource
