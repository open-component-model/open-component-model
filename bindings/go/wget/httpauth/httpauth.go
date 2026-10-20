// Package httpauth applies wget credentials to outgoing HTTP requests.
package httpauth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	"ocm.software/open-component-model/bindings/go/runtime"
	credv1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
)

// Apply applies OCM credentials to the HTTP request or client.
// Supported credential types:
//   - certificate + privateKey (+ optional certificateAuthority): mTLS client
//     certificate, applied to the transport independently of the header auth below
//   - identityToken: Bearer token in the Authorization header
//   - username + password: HTTP Basic Authentication
//
// The mTLS client certificate composes with the header-based auth. Bearer and
// Basic both use the Authorization header and are mutually exclusive; the bearer
// token takes precedence when both are set.
//
// Both WgetCredentials/v1 and legacy DirectCredentials/v1 are accepted.
func Apply(ctx context.Context, req *http.Request, client **http.Client, credentials runtime.Typed) error {
	if credentials == nil {
		return nil
	}

	creds, err := credv1.ConvertToWgetCredentials(credentials)
	if err != nil {
		return fmt.Errorf("error converting credentials: %w", err)
	}

	// The mTLS client certificate is a transport-layer credential and is applied
	// independently of the header-based authentication below, so it can be
	// combined with a bearer token or basic auth.
	if creds.Certificate != "" {
		// A client certificate only takes effect during a TLS handshake. Over
		// plain HTTP it is silently unused, so warn the user it has no effect.
		if req.URL.Scheme != "https" {
			slog.WarnContext(ctx, "client certificate credentials provided for a non-HTTPS URL", "scheme", req.URL.Scheme)
		}

		cert, err := tls.X509KeyPair([]byte(creds.Certificate), []byte(creds.PrivateKey))
		if err != nil {
			return fmt.Errorf("invalid certificate/privateKey for mTLS: %w", err)
		}

		var rootCAs *x509.CertPool
		if creds.CertificateAuthority != "" {
			rootCAs = x509.NewCertPool()
			if !rootCAs.AppendCertsFromPEM([]byte(creds.CertificateAuthority)) {
				return fmt.Errorf("failed to parse certificateAuthority PEM")
			}
		}

		// Install the certificate on the client's own transports so its
		// configuration (timeouts, retries, proxy, compression, TLS) is kept.
		var raisedMinVersion uint16
		cloned, err := ocmhttp.CloneTransports(*client, func(tr *http.Transport) {
			// The clone serves this request only and is then dropped, so its
			// idle connections could never be reused; close them instead of
			// leaving them open until IdleConnTimeout, which may be unlimited.
			tr.DisableKeepAlives = true
			cfg := tr.TLSClientConfig
			cfg.Certificates = []tls.Certificate{cert}
			// Go ignores Certificates when GetClientCertificate is set, which
			// would keep the explicit credential from being presented.
			cfg.GetClientCertificate = nil
			if rootCAs != nil {
				cfg.RootCAs = rootCAs
			}
			if cfg.MinVersion < tls.VersionTLS12 {
				// Zero means the Go default, which is already TLS 1.2 for clients.
				if cfg.MinVersion != 0 {
					raisedMinVersion = cfg.MinVersion
				}
				cfg.MinVersion = tls.VersionTLS12
			}
		})
		if err != nil {
			return fmt.Errorf("cannot apply mTLS client certificate: %w", err)
		}
		if raisedMinVersion != 0 {
			slog.WarnContext(ctx, "raising the client's minimum TLS version to TLS 1.2 for the mTLS client certificate",
				"configured", tls.VersionName(raisedMinVersion))
		}
		*client = cloned
	}

	// IdentityToken (Bearer) and Username/Password (Basic) both set the
	// Authorization header, so at most one applies. IdentityToken takes
	// precedence when both are set.
	if creds.IdentityToken != "" && creds.Username != "" {
		slog.WarnContext(ctx, "both bearer token and basic auth credentials provided; using the bearer token and ignoring basic auth")
	}
	switch {
	case creds.IdentityToken != "":
		req.Header.Set("Authorization", "Bearer "+creds.IdentityToken)
	case creds.Username != "":
		req.SetBasicAuth(creds.Username, creds.Password)
	default:
		return nil
	}

	// net/http forwards the Authorization header on redirects to the same host, even from
	// https to http, which would send the credentials in clear text.
	stripped := **client
	stripped.CheckRedirect = stripAuthOnDowngrade(stripped.CheckRedirect)
	*client = &stripped
	return nil
}

// stripAuthOnDowngrade wraps a redirect policy so that a redirect from an https request to
// a non-https URL does not carry the Authorization header. next is the wrapped policy; nil
// is net/http's default of following at most 10 redirects.
func stripAuthOnDowngrade(next func(*http.Request, []*http.Request) error) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			req.Header.Del("Authorization")
		}
		if next != nil {
			return next(req, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
}
