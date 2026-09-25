// Package npm provides access to npm packages: the NPM access specification,
// typed credentials, the NpmRegistry consumer identity, and the resource
// repository that downloads a package tarball.
//
// The access spec preserves the registry, package and version values produced by
// OCM v1. Version selectors such as dist-tags are passed to the registry's version
// endpoint; there is no client-side tag or range resolution. Exact versions are
// recommended for reproducibility. Packument fallback looks up the selector as an
// exact key in the versions map.
//
// [ocm.software/open-component-model/bindings/go/npm/repository.ResourceRepository]
// is the entry point. It resolves the version metadata of a package, downloads
// the tarball that dist.tarball points to, and verifies published checksums;
// a mismatch fails the download. Missing checksums produce a warning rather than
// a failure. Uploading to a registry is not supported.
//
// Verification follows npm: the strongest algorithm in dist.integrity is checked
// against any of the digests published for it, and dist.shasum is used only when
// dist.integrity is absent or empty. Malformed integrity or integrity containing
// only unsupported algorithms is an error rather than a skipped check.
//
// The registry is asked for the version document at <registry>/<package>/<version>
// first and for the full packument at <registry>/<package> as a fallback. The
// packument is the only path npm itself uses and the only one Nexus serves, so
// client errors other than 401/403 on the version URL fall back to it. The abbreviated packument
// is requested where the registry offers it.
//
// The repository uses the shared OCM HTTP client with its configured timeouts,
// retries and TLS settings, or a client supplied through [repository.WithHTTPClient].
//
// Explicit file:// registries read metadata and tarballs from the local filesystem.
// As in OCM v1, the prefix is stripped literally, without percent decoding; relative
// paths are relative to the process working directory. Local registries need no
// credential identity. For safety, HTTP(S) registries cannot point to file://
// tarballs: only an explicitly file-backed registry permits local file reads.
//
// Tarballs are streamed into a file under the configured temp folder rather than
// buffered, so memory use stays flat regardless of package size. That file
// outlives the download and is owned by the returned blob. Callers can close the
// blob through io.Closer to remove the file promptly; abandoned blobs also reclaim
// their files when they become unreachable. There is no size limit by default;
// [repository.WithMaxDownloadSize] adds one.
//
// Credentials are optional and resolved through the NpmRegistry consumer
// identity, which is derived from the registry URL joined with the package name.
// Credentials can therefore be configured for a whole registry or narrowed to a
// scope or a single package. Basic auth and a bearer token are supported; both
// use the Authorization header and are mutually exclusive, with basic auth taking
// precedence because it is what OCM v1 read with. Because dist.tarball comes out
// of a metadata document, credentials are only sent to a tarball served by the
// registry host itself, and a redirect that would carry them from https to http
// is refused.
//
// The access type is registered under NPM/v1 and NPM plus the legacy OCM v1 names
// npm and npm/v1. Credentials and identity are registered under their versioned
// and unversioned names.
package npm
