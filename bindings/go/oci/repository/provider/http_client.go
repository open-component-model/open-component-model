package provider

import (
	"net"
	"net/http"
	"time"

	"oras.land/oras-go/v2/registry/remote/retry"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/http/v1alpha1/spec"
)

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

// NewHTTPClient creates a new HTTP client built on top of the ORAS retry client.
// Timeout and retry fields from the provided config override the defaults.
// A User-Agent header is injected only when WithHTTPUserAgent is provided.
//
// Transport chain:
//
//	http.Client → [userAgentTransport] → retry.Transport → http.Transport
//
// Each request flows through the chain left to right:
//  1. userAgentTransport sets the User-Agent header (optional).
//  2. retry.Transport retries failed requests based on the retry policy.
//  3. http.Transport handles the actual TCP/TLS connection with configured timeouts.
func NewHTTPClient(opts ...HTTPClientOption) *http.Client {
	options := &HTTPClientOptions{}
	for _, opt := range opts {
		opt(options)
	}
	// Start from a fresh retry client so we never mutate the global default.
	httpClient := retry.NewClient()
	retryTransport := httpClient.Transport.(*retry.Transport)

	// Override transport-level timeouts when config is provided.
	if options.config != nil {
		retryTransport.Base = newTransport(options.config)
		if options.config.Timeout != nil {
			httpClient.Timeout = time.Duration(*options.config.Timeout)
		}
		policy := newRetryPolicy(options.config)
		retryTransport.Policy = func() retry.Policy { return policy }
	}

	// Wrap with a User-Agent injector only when explicitly provided.
	if options.userAgent != "" {
		httpClient.Transport = &userAgentTransport{
			base:      retryTransport,
			userAgent: options.userAgent,
		}
	}

	return httpClient
}

// newTransport creates an http.Transport with timeouts from the config.
func newTransport(cfg *httpv1alpha1.Config) *http.Transport {
	dialer := &net.Dialer{}
	if cfg.TCPDialTimeout != nil {
		dialer.Timeout = time.Duration(*cfg.TCPDialTimeout)
	}
	if cfg.TCPKeepAlive != nil {
		dialer.KeepAlive = time.Duration(*cfg.TCPKeepAlive)
	}

	transport := &http.Transport{
		DialContext: dialer.DialContext,
	}
	if cfg.TLSHandshakeTimeout != nil {
		transport.TLSHandshakeTimeout = time.Duration(*cfg.TLSHandshakeTimeout)
	}
	if cfg.ResponseHeaderTimeout != nil {
		transport.ResponseHeaderTimeout = time.Duration(*cfg.ResponseHeaderTimeout)
	}
	if cfg.IdleConnTimeout != nil {
		transport.IdleConnTimeout = time.Duration(*cfg.IdleConnTimeout)
	}
	return transport
}

// newRetryPolicy creates a retry policy from scratch using ORAS defaults,
// overriding fields explicitly set in config.
func newRetryPolicy(cfg *httpv1alpha1.Config) *retry.GenericPolicy {
	policy := &retry.GenericPolicy{
		Retryable: retry.DefaultPredicate,
		Backoff:   retry.DefaultBackoff,
		MinWait:   time.Duration(httpv1alpha1.DefaultRetryMinWait),
		MaxWait:   time.Duration(httpv1alpha1.DefaultRetryMaxWait),
		MaxRetry:  httpv1alpha1.DefaultRetryMaxRetry,
	}
	if cfg.RetryMinWait != nil {
		policy.MinWait = time.Duration(*cfg.RetryMinWait)
	}
	if cfg.RetryMaxWait != nil {
		policy.MaxWait = time.Duration(*cfg.RetryMaxWait)
	}
	if cfg.RetryMaxRetry != nil {
		policy.MaxRetry = *cfg.RetryMaxRetry
	}
	return policy
}
