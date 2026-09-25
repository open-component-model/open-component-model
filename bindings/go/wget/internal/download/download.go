// Package download contains the shared HTTP download logic for the wget bindings.
// Callers convert their own specification into a [Request] and invoke [Download],
// so the transport, credential handling and size limiting live in exactly one place.
package download

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"

	"ocm.software/open-component-model/bindings/go/wget/httpauth"
)

const tempFilePattern = "ocm-wget-download-*"

// Request describes a single HTTP download and carries the primitive parameters
// of the request.
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

// Download performs the HTTP request described by req and returns the response body
// as a blob backed by a file on disk. Bodies are streamed rather than buffered, so
// memory use stays flat regardless of response size; the file is created under the
// directory given by [WithTempDir] and outlives this call.
//
// The returned [Blob] owns that file: callers should [Blob.Close] it once they are
// done, and an unclosed blob has its file removed when it becomes unreachable.
//
// The HTTP client, credentials and maximum download size are supplied via options;
// see [WithClient], [WithCredentials] and [WithMaxDownloadSize].
func Download(ctx context.Context, req Request, opts ...Option) (_ *Blob, err error) {
	o := &option{}
	for _, opt := range opts {
		opt(o)
	}

	resp, safeURL, err := open(ctx, req, o)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.WarnContext(ctx, "failed to close HTTP response body", "error", err)
		}
	}()

	// A nil option means "use the default"; a zero or negative value disables the limit.
	maxDownloadSize := DefaultMaxDownloadSize
	if o.MaxDownloadSize != nil {
		maxDownloadSize = *o.MaxDownloadSize
	}

	// When the server announces the size up front, an oversized body is rejected
	// before any of it is transferred. ContentLength is negative when unknown.
	if maxDownloadSize > 0 && resp.ContentLength > maxDownloadSize {
		return nil, fmt.Errorf("response body from %s exceeds maximum allowed size of %d bytes", safeURL, maxDownloadSize)
	}

	respBody := io.Reader(resp.Body)
	if maxDownloadSize > 0 {
		respBody = io.LimitReader(resp.Body, maxDownloadSize+1)
	}

	file, err := os.CreateTemp(o.TempDir, tempFilePattern)
	if err != nil {
		return nil, fmt.Errorf("error creating temporary file for %s: %w", safeURL, err)
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
		return nil, fmt.Errorf("error writing response body from %s to %s: %w", safeURL, path, err)
	}

	if maxDownloadSize > 0 && written > maxDownloadSize {
		return nil, fmt.Errorf("response body from %s exceeds maximum allowed size of %d bytes", safeURL, maxDownloadSize)
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
		return nil, fmt.Errorf("error creating blob for %s from %s: %w", safeURL, path, err)
	}
	b.SetMediaType(mediaType)
	b.headers = resp.Header
	b.digests = digests

	return b, nil
}

// Open performs the HTTP request described by req and returns the response with its body
// unread, so callers can stream it. Only the client and credentials options apply. The
// caller must close the response body.
func Open(ctx context.Context, req Request, opts ...Option) (*http.Response, error) {
	o := &option{}
	for _, opt := range opts {
		opt(o)
	}
	resp, _, err := open(ctx, req, o)
	return resp, err
}

// open sends the request and checks the response status. It also returns the request URL
// without userinfo, query and fragment for use in errors and logs.
func open(ctx context.Context, req Request, o *option) (*http.Response, string, error) {
	if req.URL == "" {
		return nil, "", fmt.Errorf("url is required")
	}
	client := o.Client
	if client == nil {
		client = http.DefaultClient
	}

	parsedURL, err := url.Parse(req.URL)
	if err != nil {
		return nil, "", fmt.Errorf("invalid url: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, "", fmt.Errorf("unsupported url scheme %q: only http and https are allowed", parsedURL.Scheme)
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
		return nil, "", fmt.Errorf("error creating HTTP request: %w", err)
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

	if err := httpauth.Apply(ctx, httpReq, &client, o.Credentials); err != nil {
		return nil, "", fmt.Errorf("error applying credentials: %w", err)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, "", fmt.Errorf("error performing HTTP request to %s: %w", safeURL.String(), err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, "", fmt.Errorf("HTTP request to %s returned status %d", safeURL.String(), resp.StatusCode)
	}
	return resp, safeURL.String(), nil
}

func CloneClientWithNoRedirect(original *http.Client) *http.Client {
	c := *original
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &c
}
