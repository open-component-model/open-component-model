// Package npm provides access to npm packages: the NPM access specification,
// typed credentials, the NpmRegistry consumer identity, and the resource
// repository that downloads a package tarball.
//
// The access spec addresses one exact package version in one registry. Version
// ranges and dist-tags are rejected, so a component version always resolves to
// the same tarball.
//
// [ocm.software/open-component-model/bindings/go/npm/repository.ResourceRepository]
// is the entry point. It resolves the version metadata of a package, downloads
// the tarball that dist.tarball points to, and verifies it before handing it out;
// a mismatch fails the download. Uploading to a registry is not supported.
//
// Verification follows npm: the strongest algorithm in dist.integrity is checked
// against any of the digests published for it, and dist.shasum is used only when
// dist.integrity carries nothing usable. A dist.integrity in an algorithm this
// version cannot compute is an error rather than a skipped check.
//
// The registry is asked for the version document at <registry>/<package>/<version>
// first and for the full packument at <registry>/<package> as a fallback. The
// packument is the only path npm itself uses and the only one Nexus serves, so
// any client error on the version URL falls back to it. The abbreviated packument
// is requested where the registry offers it.
//
// The HTTP client is supplied by the caller through [repository.WithHTTPClient],
// so the global OCM http configuration (timeouts, retries, TLS) applies; without
// one, http.DefaultClient is used, which imposes no timeout.
//
// Tarballs are streamed into a file under the configured temp folder rather than
// buffered, so memory use stays flat regardless of package size. The returned
// blob owns that file: closing it removes the file, and a blob that is dropped
// without being closed has its file removed once it becomes unreachable, so
// downloads do not pile up in a long-running process. There is no size limit by
// default; [repository.WithMaxDownloadSize] adds one.
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
