package maven

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	credv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Client performs authenticated HTTP(S) transport of Maven artifacts and their
// checksum siblings. It is a thin wrapper around *http.Client that translates
// runtime.Typed credentials into Authorization headers; all OCM-level policy
// (access conversion, checksum verification, resource updates) lives with the
// caller.
type Client struct {
	http *http.Client
}

// NewClient wraps httpClient for Maven transport. A nil httpClient defaults to
// http.DefaultClient.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{http: httpClient}
}

// Get performs an authenticated GET and returns the body together with the HTTP
// status code.
func (c *Client) Get(ctx context.Context, url string, credentials runtime.Typed) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("error creating request for %q: %w", url, err)
	}
	if err := c.applyCredentials(req, credentials); err != nil {
		return nil, 0, fmt.Errorf("error applying credentials for %q: %w", url, err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("error reading body of %q: %w", url, err)
	}
	return body, resp.StatusCode, nil
}

// Put performs an authenticated PUT of data with the given content type. Any
// non-2xx response is returned as an error.
func (c *Client) Put(ctx context.Context, url string, data []byte, contentType string, credentials runtime.Typed) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("error creating PUT request for %q: %w", url, err)
	}
	req.Header.Set("Content-Type", contentType)
	if err := c.applyCredentials(req, credentials); err != nil {
		return err
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

// credentialScheme converts runtime.Typed credentials (incl. DirectCredentials).
var credentialScheme = func() *runtime.Scheme {
	s := runtime.NewScheme()
	credv1.MustRegister(s)
	return s
}()

// applyCredentials sets Authorization headers on req from runtime.Typed credentials.
// A non-empty accessToken yields Bearer auth; otherwise username/password yields Basic auth.
func (c *Client) applyCredentials(req *http.Request, credentials runtime.Typed) error {
	if credentials == nil || credentials.GetType().String() == "" {
		return nil
	}
	typed, err := credentialScheme.NewObject(credentials.GetType())
	if err != nil {
		return fmt.Errorf("error resolving credential type: %w", err)
	}
	if err := credentialScheme.Convert(credentials, typed); err != nil {
		return fmt.Errorf("error converting credentials: %w", err)
	}
	direct, ok := typed.(*credv1.DirectCredentials)
	if !ok {
		return fmt.Errorf("unsupported credential type for maven: %v", typed.GetType())
	}
	props := direct.Properties
	if token := props["accessToken"]; token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
	if username := props["username"]; username != "" {
		req.SetBasicAuth(username, props["password"])
		return nil
	}
	return fmt.Errorf("maven credentials present but neither accessToken nor username is set")
}
