// Package download contains the shared HTTP download logic for the wget bindings.
// Callers convert their own specification into a [Request] and invoke [Download],
// so the transport, credential handling and size limiting live in exactly one place.
//
// The returned [Blob] streams the HTTP response: nothing is buffered in memory or
// written to disk. Checks over the content (size limit, digests, [Verifier]) run
// in the blob's reader and surface from its Read and Close.
package download

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	"ocm.software/open-component-model/bindings/go/wget/httpauth"
)

// Computed holds the digests a reader computed over a response, keyed by
// [DigestAlgorithm.Name] with lowercase hex values.
type Computed struct {
	// Content covers the bytes the reader returned.
	Content map[string]string
	// Pinned is the digest set with [Blob.SetPrecalculatedDigest], or empty. The
	// content has to match it.
	Pinned string
}

// Verifier checks a response body that a reader consumed in full. header is the
// header of the response the body came from.
type Verifier func(header http.Header, computed Computed) error

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

// Download sends the HTTP request described by req and returns a blob streaming
// the response body. The body is not read here: the first [Blob.ReadCloser]
// reads the already open response, and every later one sends the request again.
//
// The status code and an announced Content-Length above the maximum download size
// are checked before returning. A body that exceeds the limit while streaming, a
// body shorter than announced and a failing [Verifier] fail the reader instead.
//
// The returned [Blob] holds the open response until it is read: callers that do
// not read it should [Blob.Close] it, and an abandoned unread blob releases the
// response once it becomes unreachable.
//
// The HTTP client, credentials and maximum download size are supplied via options;
// see [WithClient], [WithCredentials] and [WithMaxDownloadSize].
func Download(ctx context.Context, req Request, opts ...Option) (*Blob, error) {
	o := &option{}
	for _, opt := range opts {
		opt(o)
	}

	// A nil option means "use the default"; a zero or negative value disables the limit.
	maxDownloadSize := DefaultMaxDownloadSize
	if o.MaxDownloadSize != nil {
		maxDownloadSize = *o.MaxDownloadSize
	}

	f, err := newFetcher(ctx, req, o, maxDownloadSize)
	if err != nil {
		return nil, err
	}
	rep, err := f.send(f.request)
	if err != nil {
		return nil, err
	}

	mediaType := req.MediaType
	if mediaType == "" {
		mediaType = rep.resp.Header.Get("Content-Type")
	}
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}

	return newBlob(f, rep, mediaType, o), nil
}

// fetcher sends the request of a download.
type fetcher struct {
	client *http.Client
	// request is the authenticated request; it is sent again for every re-read.
	request *http.Request
	// safeURL is the request URL without userinfo, query and fragment, for use
	// in errors and logs.
	safeURL string
	maxSize int64 // <=0 means unlimited
}

// newFetcher builds the authenticated request and the client to send it with.
func newFetcher(ctx context.Context, req Request, o *option, maxSize int64) (*fetcher, error) {
	if req.URL == "" {
		return nil, fmt.Errorf("url is required")
	}
	client := o.Client
	if client == nil {
		client = ocmhttp.New()
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

	// A bytes.Reader body lets the request set GetBody, so it can be sent again.
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
	// A client with compression enabled (e.g. a plain *http.Client injected via
	// WithClient) adds Accept-Encoding: gzip and transparently decodes the body,
	// so the blob would no longer be the served bytes that sizes, advertised
	// checksums and pinned digests describe. An explicit header disables that.
	if httpReq.Header.Get("Accept-Encoding") == "" {
		httpReq.Header.Set("Accept-Encoding", "identity")
	}
	if req.NoRedirect {
		client = CloneClientWithNoRedirect(client)
	}

	if err := httpauth.Apply(ctx, httpReq, &client, o.Credentials); err != nil {
		return nil, fmt.Errorf("error applying credentials: %w", err)
	}
	return &fetcher{
		client:  client,
		request: httpReq,
		safeURL: safeURL.String(),
		maxSize: maxSize,
	}, nil
}

// clone returns a copy of the request that can be sent again.
func (f *fetcher) clone() (*http.Request, error) {
	r := f.request.Clone(f.request.Context())
	if f.request.GetBody != nil {
		body, err := f.request.GetBody()
		if err != nil {
			return nil, fmt.Errorf("error recreating request body for %s: %w", f.safeURL, err)
		}
		r.Body = body
	}
	return r, nil
}

// send performs r, checks the response status and checks the content size
// against the maximum. On success the caller owns the response body.
func (f *fetcher) send(r *http.Request) (representation, error) {
	resp, err := f.client.Do(r)
	if err != nil {
		return representation{}, fmt.Errorf("error performing HTTP request to %s: %w", f.safeURL, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return representation{}, fmt.Errorf("HTTP request to %s returned status %d", f.safeURL, resp.StatusCode)
	}
	rep := representation{resp: resp, size: bodySize(resp)}
	// When the content size is known up front, oversized content is rejected
	// before any of it is transferred.
	if f.maxSize > 0 && rep.size > f.maxSize {
		_ = resp.Body.Close()
		return representation{}, f.tooLarge()
	}
	return rep, nil
}

func (f *fetcher) tooLarge() error {
	return fmt.Errorf("response body from %s exceeds maximum allowed size of %d bytes", f.safeURL, f.maxSize)
}

func CloneClientWithNoRedirect(original *http.Client) *http.Client {
	c := *original
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &c
}
