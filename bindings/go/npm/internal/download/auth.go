package download

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"

	credv1 "ocm.software/open-component-model/bindings/go/npm/spec/credentials/v1"
)

// acceptMetadata asks for the abbreviated packument, which carries the dist
// section this package needs without the readme and other prose of the full
// document. A registry that does not know the type answers with the full
// document, which is decoded just the same.
const acceptMetadata = "application/vnd.npm.install-v1+json; q=1.0, application/json; q=0.8, */*"

// get performs a GET for a registry metadata document, applying credentials when
// configured.
func get(ctx context.Context, rawURL string, creds *credv1.NPMCredentials, opts Options) (*http.Response, error) {
	if strings.HasPrefix(rawURL, "file://") {
		return localResponse(ctx, rawURL)
	}
	return do(ctx, rawURL, creds, opts, true, func(req *http.Request) {
		req.Header.Set("Accept", acceptMetadata)
	})
}

// getTarball performs a GET for dist.tarball. Credentials are only sent when the
// tarball is served by the registry host itself: the URL comes out of a metadata
// document, so a registry could otherwise point the download at a third-party
// host and collect the credentials configured for the registry.
func getTarball(ctx context.Context, rawURL, registry string, creds *credv1.NPMCredentials, opts Options) (*http.Response, error) {
	if strings.HasPrefix(rawURL, "file://") {
		if !strings.HasPrefix(registry, "file://") {
			return nil, fmt.Errorf("file tarballs require an explicitly file-backed registry")
		}
		return localResponse(ctx, rawURL)
	}
	trusted, err := sameHost(registry, rawURL)
	if err != nil {
		return nil, err
	}
	if !trusted && creds != nil {
		slog.WarnContext(ctx, "not sending registry credentials to a tarball host that differs from the registry host",
			"registry", safeURLString(registry), "tarball", safeURLString(rawURL))
	}

	return do(ctx, rawURL, creds, opts, trusted, func(req *http.Request) {
		// Without this the transport asks for gzip and transparently decompresses
		// the response. A tarball is already gzip, so a registry that labels it
		// Content-Encoding: gzip would have a layer stripped here and the bytes
		// would no longer be the ones the published checksum covers. Asking for
		// identity also keeps Content-Length intact, so an oversized tarball is
		// rejected before it is transferred.
		req.Header.Set("Accept-Encoding", "identity")
	})
}

// localResponse keeps local files on the same streaming, size-limit and checksum
// path as HTTP bodies. Strip only the prefix: OCM v1 treats file paths literally.
func localResponse(ctx context.Context, rawURL string) (*http.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(strings.TrimPrefix(rawURL, "file://"))
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: info.Size(),
		Body:          &localBody{file: file, ctx: ctx},
	}, nil
}

type localBody struct {
	file *os.File
	ctx  context.Context
}

func (b *localBody) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	return b.file.Read(p)
}

func (b *localBody) Close() error {
	return b.file.Close()
}

func do(ctx context.Context, rawURL string, creds *credv1.NPMCredentials, opts Options, withCredentials bool, prepare func(*http.Request)) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating HTTP request: %w", redact(err))
	}

	prepare(req)

	client := opts.client()
	if withCredentials && creds != nil {
		if err := applyCredentials(ctx, req, creds); err != nil {
			return nil, fmt.Errorf("error applying credentials: %w", err)
		}
	}
	client = secureRedirects(client, req.URL, req.Header.Get("Authorization") != "" || req.URL.User != nil)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error performing HTTP request to %s: %w", safeURL(req.URL), redact(err))
	}

	return resp, nil
}

// sameHost reports whether rawURL is served by the same scheme, host and port as
// the registry. A plain-HTTP tarball for an HTTPS registry counts as a different
// host, so credentials are never downgraded onto an unencrypted connection.
func sameHost(registry, rawURL string) (bool, error) {
	registryURL, err := registryURL(registry)
	if err != nil {
		return false, redact(err)
	}

	target, err := url.Parse(rawURL)
	if err != nil {
		return false, fmt.Errorf("invalid url %q: %w", safeURLString(rawURL), redact(err))
	}

	return sameOrigin(registryURL, target), nil
}

// port returns the port of u, defaulting to the well-known port of its scheme so
// that "https://host" and "https://host:443" are recognised as the same host.
func port(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	default:
		return ""
	}
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(a.Hostname(), b.Hostname()) && port(a) == port(b)
}

// maxRedirects is Go's default when the caller supplies no redirect policy.
const maxRedirects = 10

// secureRedirects binds credentials to the initial origin, not the previous hop.
// net/http's own policy permits subdomains and ignores port changes.
func secureRedirects(client *http.Client, initial *url.URL, credentialed bool) *http.Client {
	origin := *initial
	clone := *client
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		strip := func() {
			if !sameOrigin(&origin, req.URL) {
				req.Header.Del("Authorization")
				// Otherwise the transport can synthesize Basic authorization.
				req.URL.User = nil
			}
		}
		strip()
		if client.CheckRedirect != nil {
			if err := client.CheckRedirect(req, via); err != nil {
				return err
			}
		} else if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		// The caller can modify the redirect request, so enforce this again.
		strip()
		if credentialed && strings.EqualFold(origin.Scheme, "https") && strings.EqualFold(req.URL.Scheme, "http") {
			return fmt.Errorf("refusing to follow a redirect from https to http for %s while sending credentials", safeURL(&origin))
		}
		return nil
	}
	return &clone
}

// applyCredentials applies npm credentials to the request. Supported credentials:
//   - username + password: HTTP basic authentication
//   - token: bearer token in the Authorization header, the credential an
//     "npm login" stores as _authToken
//
// Both set the Authorization header and are therefore mutually exclusive. Basic
// authentication wins, because it is the only one OCM v1 used to read from a
// registry: a configuration carrying both is a v1 configuration whose token was
// left over from a login, and honouring the token instead would turn a working
// setup into a 401.
func applyCredentials(ctx context.Context, req *http.Request, creds *credv1.NPMCredentials) error {
	// A user name without a password cannot authenticate anywhere; sending it
	// anyway turns a misconfiguration into a 401 from the registry.
	if creds.Username != "" && creds.Password == "" && creds.Token == "" {
		return fmt.Errorf("npm credentials for user %q carry no password", creds.Username)
	}

	if creds.Username != "" && creds.Password != "" && creds.Token != "" {
		slog.WarnContext(ctx, "npm credentials carry both a user name and a token; using basic authentication as OCM v1 did")
	}

	switch {
	case creds.Username != "" && creds.Password != "":
		req.SetBasicAuth(creds.Username, creds.Password)
	case creds.Token != "":
		req.Header.Set("Authorization", "Bearer "+creds.Token)
	}

	return nil
}
