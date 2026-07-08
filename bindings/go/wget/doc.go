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
// request (following redirects unless the spec disables them in the access), enforces a
// configurable maximum download size, and returns the response body as a blob:
//
//	repo := repository.NewResourceRepository(
//	    repository.WithMaxDownloadSize(50 * 1024 * 1024),
//	)
//	b, err := repo.DownloadResource(ctx, resource, credentials)
//	if err != nil {
//	    return err
//	}
//
// Credentials are optional. When supplied as
// [ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1.WgetCredentials]
// they are applied to the outgoing request. An mTLS client certificate is a
// transport-layer credential and is applied independently, so it can be combined
// with header-based authentication. Basic auth and a bearer token both use the
// Authorization header and are mutually exclusive; the bearer token takes
// precedence when both are set.
// [ocm.software/open-component-model/bindings/go/wget/access.WgetAccess] derives
// the credential consumer identity from a resource's URL, so a credential
// resolver can look up matching credentials before the download.
//
// It also implements the "wget" constructor input method in
// [ocm.software/open-component-model/bindings/go/wget/input.InputMethod], which
// references an HTTP/S URL declared on a resource in a component-constructor.yaml
// through a
// [ocm.software/open-component-model/bindings/go/wget/spec/input/v1.Wget] input
// spec. The input spec carries the same request details as the access spec and
// supports two output modes:
//
//   - local blob (default): the content is downloaded during construction and
//     stored as a local blob, making the component version self-contained.
//   - access spec (the input sets Reference): the content is not downloaded; the
//     resource is stored with a wget access spec pointing at the URL, so it is
//     fetched lazily when the resource is later accessed.
//
// The input method shares the download transport, credential handling and size
// limiting used by the access type, so both behave identically for a given URL
// and credentials, and both derive the credential consumer identity from the URL.
//
// The wire types are each registered in their package scheme for typed
// conversion. Both the versioned (wget/v1) and unversioned (wget) type names are
// registered, and legacy upper-case access specs remain parsable because JSON
// field matching is case-insensitive.
package wget
