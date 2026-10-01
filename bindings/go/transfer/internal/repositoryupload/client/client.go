// Package client sends the authenticated HTTP requests of a repository upload to the target
// server. Errors never carry userinfo, query or fragment of a request URL, see [RedactURL].
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/httpauth"
)

const (
	// maxErrorBodyBytes bounds how much of a non-2xx response body is read into an error.
	maxErrorBodyBytes = 4 << 10
	// maxJSONBytes bounds how much of a successful JSON response body is decoded; search pages
	// exceed maxErrorBodyBytes.
	maxJSONBytes = 1 << 20
)

// Client sends requests with the upload credentials.
type Client struct {
	httpConfig *httpv1alpha1.Config
	creds      runtime.Typed
}

// New returns a client sending requests configured by httpConfig with creds (nil for anonymous
// requests), see [httpauth.Apply].
func New(httpConfig *httpv1alpha1.Config, creds runtime.Typed) *Client {
	return &Client{httpConfig: httpConfig, creds: creds}
}

// Do sends a single request. It does not follow redirects of requests other than GET and HEAD.
// The caller closes the response body.
func (c *Client) Do(ctx context.Context, method, target string, body io.Reader, size int64, header http.Header) (*http.Response, error) {
	safe := RedactURL(target)
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, fmt.Errorf("failed creating %s request for %s: %w", method, safe, err)
	}
	if size >= 0 {
		req.ContentLength = size
	}
	for k, v := range header {
		req.Header[k] = v
	}
	client := ocmhttp.New(ocmhttp.WithConfig(c.httpConfig))
	if method != http.MethodGet && method != http.MethodHead {
		// Following a redirect turns PUT and POST into a GET, so a redirect to e.g. a login page
		// would report an upload as successful that stored nothing. The 3xx fails the request instead.
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	if err := httpauth.Apply(ctx, req, &client, c.creds); err != nil {
		return nil, fmt.Errorf("failed applying target credentials: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		// client.Do wraps errors in a *url.Error carrying the full request URL.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			urlErr.URL = safe
		}
		return nil, fmt.Errorf("%s %s failed: %w", method, safe, err)
	}
	return resp, nil
}

// Send sends a single request and fails on a non-2xx response. A successful JSON response is
// decoded into out, if set.
func (c *Client) Send(ctx context.Context, method, target string, body io.Reader, size int64, header http.Header, out any) error {
	resp, err := c.Do(ctx, method, target, body, size, header)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	safe := RedactURL(target)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		if msg := strings.TrimSpace(string(excerpt)); msg != "" {
			return fmt.Errorf("%s %s returned status %d: %s", method, safe, resp.StatusCode, msg)
		}
		return fmt.Errorf("%s %s returned status %d", method, safe, resp.StatusCode)
	}
	if out != nil {
		if err := DecodeJSON(resp.Body, out); err != nil {
			return fmt.Errorf("failed decoding response of %s %s: %w", method, safe, err)
		}
	}
	return nil
}

// GetJSON GETs target and decodes a 200 JSON body into out. A 404 yields found=false; any other
// non-200 status yields "GET <redacted target> returned status <code>".
func (c *Client) GetJSON(ctx context.Context, target string, out any) (found bool, err error) {
	resp, err := c.Do(ctx, http.MethodGet, target, nil, -1, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("GET %s returned status %d", RedactURL(target), resp.StatusCode)
	}
	if err := DecodeJSON(resp.Body, out); err != nil {
		return false, fmt.Errorf("failed decoding response of GET %s: %w", RedactURL(target), err)
	}
	return true, nil
}

// PutBlob streams content to target and returns the digest of the bytes read, see
// [DigestAlgorithm], and whether content was read to its end. A successful response body is
// decoded into out, if set.
func (c *Client) PutBlob(ctx context.Context, target string, content blob.ReadOnlyBlob, known digest.Digest, header http.Header, out any) (digest.Digest, bool, error) {
	rc, err := content.ReadCloser()
	if err != nil {
		return "", false, fmt.Errorf("failed opening content: %w", err)
	}
	defer func() { _ = rc.Close() }()
	size := blob.SizeUnknown
	if sized, ok := content.(blob.SizeAware); ok {
		size = sized.Size()
	}
	digester := DigestAlgorithm(known).Digester()
	body := &eofReader{r: rc}
	err = c.Send(ctx, http.MethodPut, target, io.TeeReader(body, digester.Hash()), size, header, out)
	return digester.Digest(), body.eof, err
}

// DigestAlgorithm is the algorithm uploaded content is hashed with: that of the digest the
// content is known to have, else SHA-256.
func DigestAlgorithm(known digest.Digest) digest.Algorithm {
	if known == "" {
		return digest.SHA256
	}
	return known.Algorithm()
}

// DecodeJSON decodes a JSON response body into out; an empty body leaves out unchanged.
func DecodeJSON(body io.Reader, out any) error {
	if err := json.NewDecoder(io.LimitReader(body, maxJSONBytes)).Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// RedactURL strips userinfo, query and fragment so credentials or presigned parameters never
// reach logs or errors.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid url>"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// eofReader records whether its reader returned io.EOF, i.e. was read to its end.
type eofReader struct {
	r   io.Reader
	eof bool
}

func (e *eofReader) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if errors.Is(err, io.EOF) {
		e.eof = true
	}
	return n, err
}
