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

		tlsCfg := &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cert},
		}

		if creds.CertificateAuthority != "" {
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM([]byte(creds.CertificateAuthority)) {
				return fmt.Errorf("failed to parse certificateAuthority PEM")
			}
			tlsCfg.RootCAs = pool
		}

		// Clone the client and install an mTLS transport, preserving the
		// original transport's settings (proxy, timeouts, connection pooling).
		existing := *client
		var baseTransport *http.Transport
		if t, ok := existing.Transport.(*http.Transport); ok && t != nil {
			baseTransport = t.Clone()
		} else {
			if t, ok = http.DefaultTransport.(*http.Transport); ok && t != nil {
				baseTransport = t.Clone()
			} else {
				baseTransport = &http.Transport{}
			}
		}
		baseTransport.TLSClientConfig = tlsCfg
		cloned := &http.Client{
			Timeout:       existing.Timeout,
			Jar:           existing.Jar,
			CheckRedirect: existing.CheckRedirect,
			Transport:     baseTransport,
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
