// Package download contains the shared HTTP download logic for the wget
// bindings. Callers convert their own specification into a [Request] and
// invoke [Download], so transport, credential handling and size limiting
// live in one place.
package download

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"

	"ocm.software/open-component-model/bindings/go/runtime"
	credv1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
)

const tempFilePattern = "ocm-wget-download-*"

// Request describes a single HTTP download.
type Request struct {
	// URL is the http/https endpoint.
	URL string
	// MediaType overrides the resulting blob's media type. Empty falls back to
	// the response Content-Type, then to application/octet-stream.
	MediaType string
	// Header carries additional HTTP request headers.
	Header map[string][]string
	// Verb is the HTTP method; defaults to GET.
	Verb string
	// Body is the optional request body.
	Body []byte
	// NoRedirect disables following redirects.
	NoRedirect bool
}

// Download performs the request and returns the response body as a file-backed
// blob. Bodies are streamed, so memory use is flat regardless of size.
//
// The returned [Blob] owns the temp file: callers should Close it; unclosed
// blobs have their file removed when unreachable.
func Download(ctx context.Context, req Request, opts ...Option) (_ *Blob, err error) {
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

	// safeURL strips userinfo and query params so presigned URLs and
	// credentials never leak into error messages or logs.
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
	// When digests are computed over the response body, force
	// Accept-Encoding: identity. Go's default transport otherwise auto-adds
	// gzip and transparently decompresses; hashing decoded bytes then breaks
	// RFC 9530 Content-Digest (computed over encoded bytes) and lets a mirror
	// serving compressed bytes yield a different OCM SHA-256.
	if len(o.DigestAlgorithms) > 0 {
		httpReq.Header.Set("Accept-Encoding", "identity")
	}

	if req.NoRedirect {
		client = CloneClientWithNoRedirect(client)
	}

	if err := ApplyCredentials(ctx, httpReq, &client, o.Credentials); err != nil {
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

	// nil option means "use the default"; zero or negative disables the limit.
	maxDownloadSize := DefaultMaxDownloadSize
	if o.MaxDownloadSize != nil {
		maxDownloadSize = *o.MaxDownloadSize
	}

	// Reject an oversized body up-front when Content-Length is known.
	if maxDownloadSize > 0 && resp.ContentLength > maxDownloadSize {
		return nil, fmt.Errorf("response body from %s exceeds maximum allowed size of %d bytes", safeURL.String(), maxDownloadSize)
	}

	respBody := io.Reader(resp.Body)
	if maxDownloadSize > 0 {
		respBody = io.LimitReader(resp.Body, maxDownloadSize+1)
	}

	file, err := os.CreateTemp(o.TempDir, tempFilePattern)
	if err != nil {
		return nil, fmt.Errorf("error creating temporary file for %s: %w", safeURL.String(), err)
	}
	path := file.Name()

	defer func() {
		if err != nil {
			_ = os.Remove(path)
		}
	}()

	// Hash the stream while writing it to disk so no second read is needed.
	hashers := make(map[string]hash.Hash, len(o.DigestAlgorithms))
	writers := []io.Writer{file}
	for _, alg := range o.DigestAlgorithms {
		h := alg.New()
		hashers[alg.Name] = h
		writers = append(writers, h)
	}
	dst := io.Writer(file)
	if len(writers) > 1 {
		dst = io.MultiWriter(writers...)
	}

	written, err := io.Copy(dst, respBody)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("error writing response body from %s to %s: %w", safeURL.String(), path, err)
	}

	if maxDownloadSize > 0 && written > maxDownloadSize {
		return nil, fmt.Errorf("response body from %s exceeds maximum allowed size of %d bytes", safeURL.String(), maxDownloadSize)
	}

	digests := make(map[string]string, len(hashers))
	for name, h := range hashers {
		digests[name] = hex.EncodeToString(h.Sum(nil))
	}

	mediaType := req.MediaType
	if mediaType == "" {
		mediaType = resp.Header.Get("Content-Type")
	}
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}

	b, err := newBlob(path)
	if err != nil {
		return nil, fmt.Errorf("error creating blob for %s from %s: %w", safeURL.String(), path, err)
	}
	b.SetMediaType(mediaType)
	b.headers = resp.Header
	b.digests = digests

	return b, nil
}

// ApplyCredentials applies OCM credentials to req and/or client. Supported:
//   - certificate + privateKey (+ optional certificateAuthority): mTLS
//   - identityToken: Bearer in Authorization
//   - username + password: HTTP Basic
//
// mTLS composes with header auth. Bearer and Basic are mutually exclusive on
// the Authorization header; Bearer wins when both are set. Both
// WgetCredentials/v1 and legacy DirectCredentials/v1 are accepted.
func ApplyCredentials(ctx context.Context, req *http.Request, client **http.Client, credentials runtime.Typed) error {
	if credentials == nil {
		return nil
	}

	creds, err := credv1.ConvertToWgetCredentials(credentials)
	if err != nil {
		return fmt.Errorf("error converting credentials: %w", err)
	}

	if creds.Certificate != "" {
		// mTLS is silently unused over plain HTTP; warn the user.
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

		// Clone the client so we don't mutate the caller's transport.
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

func CloneClientWithNoRedirect(original *http.Client) *http.Client {
	c := *original
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &c
}
