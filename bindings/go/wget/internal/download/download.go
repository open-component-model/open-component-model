// Package download contains the shared HTTP download logic used by both the wget
// access type (repository) and the wget input method. Neither the access spec nor
// the input spec is used here directly: each caller converts its own specification
// into a [Request] and invokes [Download], so the transport, credential handling and
// size limiting live in exactly one place.
package download

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/runtime"
	credv1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
)

// Request describes a single HTTP download. It carries the primitive parameters
// shared by the wget access spec and the wget input spec.
type Request struct {
	// URL is the http/https endpoint to download from.
	URL string
	// MediaType overrides the media type of the resulting blob. When empty the
	// response Content-Type is used, falling back to application/octet-stream.
	MediaType string
	// Header contains additional HTTP headers to send with the request.
	Header map[string][]string
	// Verb is the HTTP method to use. Defaults to GET when empty.
	Verb string
	// Body is the optional request body.
	Body []byte
	// NoRedirect disables following HTTP redirects when set.
	NoRedirect bool
}

// Download performs the HTTP request described by req and returns the response
// body as an in-memory blob. The HTTP client, credentials and maximum download
// size are supplied via options; see [WithClient], [WithCredentials] and
// [WithMaxDownloadSize].
func Download(ctx context.Context, req Request, opts ...Option) (blob.ReadOnlyBlob, error) {
	o := &option{}
	for _, opt := range opts {
		opt(o)
	}

	if req.URL == "" {
		return nil, fmt.Errorf("url is required")
	}
	client := o.Client
	if client == nil {
		client = http.DefaultClient
	}

	parsedURL, err := url.Parse(req.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("unsupported url scheme %q: only http and https are allowed", parsedURL.Scheme)
	}

	// safeURL strips userinfo and query params so presigned URLs and credentials
	// are never leaked into error messages or logs.
	safeURL := *parsedURL
	safeURL.User = nil
	safeURL.RawQuery = ""
	safeURL.Fragment = ""

	method := http.MethodGet
	if req.Verb != "" {
		method = req.Verb
	}

	var body io.Reader
	if len(req.Body) > 0 {
		body = bytes.NewReader(req.Body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, req.URL, body)
	if err != nil {
		return nil, fmt.Errorf("error creating HTTP request: %w", err)
	}

	for k, vals := range req.Header {
		for _, v := range vals {
			httpReq.Header.Add(k, v)
		}
	}

	if req.NoRedirect {
		client = cloneClientWithNoRedirect(client)
	}

	if err := applyCredentials(ctx, httpReq, &client, o.Credentials); err != nil {
		return nil, fmt.Errorf("error applying credentials: %w", err)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("error performing HTTP request to %s: %w", safeURL.String(), err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.WarnContext(ctx, "failed to close HTTP response body", "error", err)
		}
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP request to %s returned status %d", safeURL.String(), resp.StatusCode)
	}

	var data []byte
	if o.MaxDownloadSize > 0 {
		limitedReader := io.LimitReader(resp.Body, o.MaxDownloadSize+1)
		data, err = io.ReadAll(limitedReader)
		if err != nil {
			return nil, fmt.Errorf("error reading HTTP response body from %s: %w", safeURL.String(), err)
		}
		if int64(len(data)) > o.MaxDownloadSize {
			return nil, fmt.Errorf("response body from %s exceeds maximum allowed size of %d bytes", safeURL.String(), o.MaxDownloadSize)
		}
	} else {
		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("error reading HTTP response body from %s: %w", safeURL.String(), err)
		}
	}

	mediaType := req.MediaType
	if mediaType == "" {
		mediaType = resp.Header.Get("Content-Type")
	}
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}

	blobOpts := []inmemory.MemoryBlobOption{
		inmemory.WithMediaType(mediaType),
		inmemory.WithSize(int64(len(data))),
	}

	return inmemory.New(bytes.NewReader(data), blobOpts...), nil
}

// applyCredentials applies OCM credentials to the HTTP request or client.
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
func applyCredentials(ctx context.Context, req *http.Request, client **http.Client, credentials runtime.Typed) error {
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
			baseTransport = &http.Transport{}
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
	}

	return nil
}

func cloneClientWithNoRedirect(original *http.Client) *http.Client {
	return &http.Client{
		Transport: original.Transport,
		Timeout:   original.Timeout,
		Jar:       original.Jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
