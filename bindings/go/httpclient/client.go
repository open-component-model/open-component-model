package httpclient

import (
	"net/http"
	"time"

	"oras.land/oras-go/v2/registry/remote/retry"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/http/v1alpha1/spec"
)

// Options holds configuration for New.
type Options struct {
	config    *httpv1alpha1.Config
	userAgent string
}

// Option is a functional option for New.
type Option func(*Options)

// WithConfig sets the HTTP configuration (timeouts) used to build the client.
func WithConfig(cfg *httpv1alpha1.Config) Option {
	return func(o *Options) {
		o.config = cfg
	}
}

// WithUserAgent sets the User-Agent header injected on every request.
func WithUserAgent(userAgent string) Option {
	return func(o *Options) {
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

// New builds an *http.Client on top of the oras-go retry client,
// applying the transport-level timeouts from the supplied HTTP configuration.
// It is the factory counterpart to httpv1alpha1.ResolveHTTPConfig: resolve the
// config once, then hand it here to obtain a ready-to-use client.
//
// Transport chain (outermost first):
//
//		http.Client → [userAgentTransport] → retry.Transport → http.Transport
//
//	 1. userAgentTransport sets the User-Agent header (only when WithUserAgent is given).
//	 2. retry.Transport retries transient failures using oras-go's default policy.
//	 3. http.Transport carries the configured TCP/TLS/idle timeouts.
//
// A nil config (WithConfig omitted, or omitted entirely) yields a plain
// oras-go retry client with default transport timeouts.
func New(opts ...Option) *http.Client {
	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}

	// Build the retry transport directly so we never depend on the concrete
	// type of retry.NewClient().Transport, and so the global retry.DefaultClient
	// is never mutated.
	retryTransport := retry.NewTransport(nil)
	httpClient := &http.Client{Transport: retryTransport}

	if options.config != nil {
		retryTransport.Base = NewTransport(&options.config.TimeoutConfig)
		if options.config.Timeout != nil {
			httpClient.Timeout = time.Duration(*options.config.Timeout)
		}
	}

	if options.userAgent != "" {
		httpClient.Transport = &userAgentTransport{
			base:      retryTransport,
			userAgent: options.userAgent,
		}
	}

	return httpClient
}
