package download

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
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
	return do(ctx, rawURL, creds, opts, true, func(req *http.Request) {
		req.Header.Set("Accept", acceptMetadata)
	})
}

// getTarball performs a GET for dist.tarball. Credentials are only sent when the
// tarball is served by the registry host itself: the URL comes out of a metadata
// document, so a registry could otherwise point the download at a third-party
// host and collect the credentials configured for the registry.
func getTarball(ctx context.Context, rawURL, registry string, creds *credv1.NPMCredentials, opts Options) (*http.Response, error) {
	trusted, err := sameHost(registry, rawURL)
	if err != nil {
		return nil, err
	}
	if !trusted && creds != nil {
		slog.WarnContext(ctx, "not sending registry credentials to a tarball host that differs from the registry host",
			"registry", registry, "tarball", rawURL)
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

func do(ctx context.Context, rawURL string, creds *credv1.NPMCredentials, opts Options, withCredentials bool, prepare func(*http.Request)) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating HTTP request: %w", err)
	}

	prepare(req)

	client := opts.client()
	if withCredentials && creds != nil {
		if err := applyCredentials(ctx, req, creds); err != nil {
			return nil, fmt.Errorf("error applying credentials: %w", err)
		}
		client = refusingDowngrade(client)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error performing HTTP request to %s: %w", safeURL(req.URL), err)
	}

	return resp, nil
}

// sameHost reports whether rawURL is served by the same scheme, host and port as
// the registry. A plain-HTTP tarball for an HTTPS registry counts as a different
// host, so credentials are never downgraded onto an unencrypted connection.
func sameHost(registry, rawURL string) (bool, error) {
	registryURL, err := registryURL(registry)
	if err != nil {
		return false, err
	}

	target, err := url.Parse(rawURL)
	if err != nil {
		return false, fmt.Errorf("invalid url %q: %w", rawURL, err)
	}

	return strings.EqualFold(registryURL.Scheme, target.Scheme) &&
		strings.EqualFold(port(registryURL), port(target)) &&
		strings.EqualFold(registryURL.Hostname(), target.Hostname()), nil
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

// maxRedirects is Go's own default redirect limit, restated because
// [refusingDowngrade] replaces the policy that enforces it.
const maxRedirects = 10

// refusingDowngrade returns a client that refuses a redirect from https to http.
//
// Go drops the Authorization header on a redirect to a different host but not on
// one that only changes the scheme, so without this a registry could redirect a
// credentialed request onto a plain connection and read the credentials off the
// wire.
func refusingDowngrade(client *http.Client) *http.Client {
	clone := *client
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		if req.URL.Scheme == "http" && via[0].URL.Scheme == "https" {
			return fmt.Errorf("refusing to follow a redirect from https to http for %s while sending credentials", safeURL(via[0].URL))
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
