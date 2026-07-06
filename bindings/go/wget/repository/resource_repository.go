package repository

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
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/access"
	accessspec "ocm.software/open-component-model/bindings/go/wget/spec/access"
	"ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
	credv1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
)

var wgetAccess = access.NewWgetAccess()

var _ repository.ResourceRepository = (*ResourceRepository)(nil)

// ResourceRepository implements the ResourceRepository interface for wget access types.
type ResourceRepository struct {
	client          *http.Client
	maxDownloadSize int64
}

// NewResourceRepository creates a new wget resource repository.
func NewResourceRepository(opts ...Option) *ResourceRepository {
	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}
	client := options.Client
	if client == nil {
		client = http.DefaultClient
	}
	var maxSize int64
	if options.MaxDownloadSize != nil {
		maxSize = *options.MaxDownloadSize
	} else {
		maxSize = DefaultMaxDownloadSize
	}
	return &ResourceRepository{
		client:          client,
		maxDownloadSize: maxSize,
	}
}

// GetResourceRepositoryScheme returns the scheme used by the wget resource repository.
func (r *ResourceRepository) GetResourceRepositoryScheme() *runtime.Scheme {
	return accessspec.Scheme
}

// GetResourceCredentialConsumerIdentity resolves the credential consumer identity for the given resource.
func (r *ResourceRepository) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	return wgetAccess.GetResourceCredentialConsumerIdentity(ctx, resource)
}

// DownloadResource downloads a resource from the URL specified in the wget access spec.
func (r *ResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}

	wget := v1.Wget{}
	if err := accessspec.Scheme.Convert(resource.Access, &wget); err != nil {
		return nil, fmt.Errorf("error converting resource access spec: %w", err)
	}

	if wget.URL == "" {
		return nil, fmt.Errorf("url is required in wget access spec")
	}

	parsedURL, err := url.Parse(wget.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid url in wget access spec: %w", err)
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
	if wget.Verb != "" {
		method = wget.Verb
	}

	var body io.Reader
	if len(wget.Body) > 0 {
		body = bytes.NewReader(wget.Body)
	}

	req, err := http.NewRequestWithContext(ctx, method, wget.URL, body)
	if err != nil {
		return nil, fmt.Errorf("error creating HTTP request: %w", err)
	}

	for k, vals := range wget.Header {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}

	client := r.client
	if wget.NoRedirect {
		client = cloneClientWithNoRedirect(client)
	}

	if err := applyCredentials(ctx, req, &client, credentials); err != nil {
		return nil, fmt.Errorf("error applying credentials: %w", err)
	}

	resp, err := client.Do(req)
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
	if r.maxDownloadSize > 0 {
		limitedReader := io.LimitReader(resp.Body, r.maxDownloadSize+1)
		data, err = io.ReadAll(limitedReader)
		if err != nil {
			return nil, fmt.Errorf("error reading HTTP response body from %s: %w", safeURL.String(), err)
		}
		if int64(len(data)) > r.maxDownloadSize {
			return nil, fmt.Errorf("response body from %s exceeds maximum allowed size of %d bytes", safeURL.String(), r.maxDownloadSize)
		}
	} else {
		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("error reading HTTP response body from %s: %w", safeURL.String(), err)
		}
	}

	mediaType := wget.MediaType
	if mediaType == "" {
		mediaType = resp.Header.Get("Content-Type")
	}
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}

	var blobOpts []inmemory.MemoryBlobOption
	blobOpts = append(blobOpts, inmemory.WithMediaType(mediaType))
	blobOpts = append(blobOpts, inmemory.WithSize(int64(len(data))))

	return inmemory.New(bytes.NewReader(data), blobOpts...), nil
}

// UploadResource is not supported for wget access types.
func (r *ResourceRepository) UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, credentials runtime.Typed) (*descriptor.Resource, error) {
	return nil, fmt.Errorf("upload is not supported for wget access type")
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
