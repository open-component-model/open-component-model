// Package wget provides HTTP(S) access to OCM resources, both as an access type
// and as a component-constructor input method.
//
// It implements the "Wget" access type: a resource whose bytes are fetched
// from an HTTP or HTTPS endpoint described by a
// [ocm.software/open-component-model/bindings/go/wget/spec/access/v1.Wget]
// access spec. Besides the URL, the spec carries optional request details:
// media type, headers, HTTP verb, request body, and whether redirects are
// followed.
//
// [ocm.software/open-component-model/bindings/go/wget/repository.ResourceRepository]
// is the entry point. It resolves the access spec of a resource, performs the
// request (following redirects unless the spec disables them in the access), and
// returns the response body as a blob. Bodies are streamed into a file under the
// temp folder of the supplied filesystem configuration rather than buffered, so
// memory use stays flat regardless of response size. There is no size limit by
// default; [repository.WithMaxDownloadSize] adds one:
//
//	repo := repository.NewResourceRepository(filesystemConfig,
//	    repository.WithMaxDownloadSize(50 * 1024 * 1024),
//	)
//	b, err := repo.DownloadResource(ctx, resource, credentials)
//	if err != nil {
//	    return err
//	}
//	defer b.(io.Closer).Close()
//
// The returned blob owns the file it is backed by. Closing it removes that file;
// a blob that is dropped without being closed has its file removed once it becomes
// unreachable, so downloads do not pile up in the temp folder of a long-running
// process.
//
// Credentials are optional. When supplied as
// [ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1.WgetCredentials]
// they are applied to the outgoing request. An mTLS client certificate is a
// transport-layer credential and is applied independently, so it can be combined
// with header-based authentication. Basic auth and a bearer token both use the
// Authorization header and are mutually exclusive; the bearer token takes
// precedence when both are set.
// The repository derives the credential consumer identity from a resource's URL
// via GetResourceCredentialConsumerIdentity, so a credential resolver can look up
// matching credentials before the download.
//
// It also implements the "Wget" constructor input method in
// [ocm.software/open-component-model/bindings/go/wget/input.InputMethod], which
// downloads an HTTP/S URL declared on a resource in a component-constructor.yaml
// through a
// [ocm.software/open-component-model/bindings/go/wget/spec/input/v1.Wget] input
// spec and stores the content as a local blob in the component version. The input
// spec carries the same request details as the access spec, and the input method
// shares the download transport, credential handling and size limiting used by the
// access type, so both behave identically for a given URL and credentials, and
// both derive the credential consumer identity from the url.
//
// The wire types are each registered in their package scheme for typed
// conversion. Both the versioned (wget/v1) and unversioned (wget) type names are
// registered, and legacy upper-case access specs remain parsable because JSON
// field matching is case-insensitive.
package wget
