package provider

import (
	"net"
	"net/http"

	"oras.land/oras-go/v2/registry/remote/retry"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/http/v1alpha1/spec"
)

const defaultUserAgent = "OpenComponentModel"

// HTTPClientOptions holds configuration for creating an HTTP client.
type HTTPClientOptions struct {
	config    *httpv1alpha1.Config
	userAgent string
}

// HTTPClientOption is a functional option for NewHTTPClient.
type HTTPClientOption func(*HTTPClientOptions)

// WithHttpConfig sets the HTTP configuration (timeout, etc.).
func WithHttpConfig(cfg *httpv1alpha1.Config) HTTPClientOption {
	return func(o *HTTPClientOptions) {
		o.config = cfg
	}
}

// WithHTTPUserAgent sets the User-Agent header for HTTP requests.
func WithHTTPUserAgent(userAgent string) HTTPClientOption {
	return func(o *HTTPClientOptions) {
		o.userAgent = userAgent
	}
}

// userAgentTransport wraps an http.RoundTripper and injects a User-Agent header.
type userAgentTransport struct {
	base      http.RoundTripper
	userAgent string
}

func (t *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("User-Agent", t.userAgent)
	return t.base.RoundTrip(req)
}

// NewHTTPClient creates a new HTTP client with the given options applied.
// Returns retry.DefaultClient when no customization is needed.
func NewHTTPClient(opts ...HTTPClientOption) *http.Client {
	options := &HTTPClientOptions{}
	for _, opt := range opts {
		opt(options)
	}

	// Start with retry transport as base
	baseTransport := retry.DefaultClient.Transport

	// If we have config, create a custom transport with configured timeouts
	if options.config != nil {
		transport := &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   options.config.TCPDialTimeout.Value(),
				KeepAlive: options.config.TCPKeepAlive.Value(),
			}).DialContext,
			TLSHandshakeTimeout:   options.config.TLSHandshakeTimeout.Value(),
			ResponseHeaderTimeout: options.config.ResponseHeaderTimeout.Value(),
			IdleConnTimeout:       options.config.IdleConnTimeout.Value(),
		}
		baseTransport = transport
	}

	userAgent := defaultUserAgent
	if options.userAgent != "" {
		userAgent = options.userAgent
	}

	httpClient := &http.Client{
		Transport: &userAgentTransport{
			base:      baseTransport,
			userAgent: userAgent,
		},
		Timeout: 0,
	}

	if options.config != nil {
		httpClient.Timeout = options.config.Timeout.Value()
	}

	return httpClient
}
