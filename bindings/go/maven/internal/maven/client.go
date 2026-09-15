package maven

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	credsv1 "ocm.software/open-component-model/bindings/go/maven/spec/credentials/v1"
)

// Client performs authenticated HTTP(S) transport of Maven artifacts and the
// sibling files a repository publishes next to them. It is a thin wrapper
// around *http.Client that turns typed Maven credentials into an Authorization
// header; all OCM-level policy (access conversion, archive shape, resource
// updates) lives with the caller.
type Client struct {
	http *http.Client
}

// NewClient wraps httpClient for Maven transport. A nil httpClient defaults to
// http.DefaultClient; the CLI passes its configured client instead so
// timeouts, retries and TLS settings apply.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{http: httpClient}
}

// Get performs an authenticated GET and returns the response. The caller owns
// the response and must close its body. Nothing is buffered here, so a large
// artifact can be streamed straight into an archive.
func (c *Client) Get(ctx context.Context, url string, credentials *credsv1.MavenCredentials) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request for %q: %w", url, err)
	}
	if err := applyCredentials(req, credentials); err != nil {
		return nil, fmt.Errorf("error applying credentials for %q: %w", url, err)
	}
	return c.http.Do(req)
}

// Put performs an authenticated PUT that streams size bytes of body with the
// given content type. Any non-2xx response is returned as an error.
func (c *Client) Put(ctx context.Context, url string, body io.Reader, size int64, contentType string, credentials *credsv1.MavenCredentials) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, body)
	if err != nil {
		return fmt.Errorf("error creating PUT request for %q: %w", url, err)
	}
	// A body of unknown length is sent chunked, which not every repository
	// accepts, and a non-nil body with length zero counts as unknown.
	req.ContentLength = size
	if size == 0 {
		req.Body = http.NoBody
	}
	req.Header.Set("Content-Type", contentType)
	if err := applyCredentials(req, credentials); err != nil {
		return fmt.Errorf("error applying credentials for %q: %w", url, err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("error uploading to %q: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("error uploading to %q: unexpected status %d", url, resp.StatusCode)
	}
	return nil
}

// ErrUnusableCredentials is returned when credentials are present but carry
// neither a token nor a username. They are rejected rather than downgraded to
// an anonymous request, so a misconfigured secret surfaces as a configuration
// error instead of a 401 from the server.
var ErrUnusableCredentials = errors.New("maven credentials present but neither identityToken nor username is set")

// applyCredentials sets the Authorization header on req. Nil credentials mean
// an anonymous request. A token yields Bearer auth and takes precedence over
// username/password, which yield Basic auth.
func applyCredentials(req *http.Request, credentials *credsv1.MavenCredentials) error {
	if credentials == nil {
		return nil
	}
	if credentials.IdentityToken != "" {
		req.Header.Set("Authorization", "Bearer "+credentials.IdentityToken)
		return nil
	}
	if credentials.Username != "" {
		req.SetBasicAuth(credentials.Username, credentials.Password)
		return nil
	}
	return ErrUnusableCredentials
}
