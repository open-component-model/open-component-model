package provider

import (
	"net/http"
	"time"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// Options holds configuration options for the OCI repository provider.
type Options struct {
	// TempDir is the shared default temporary filesystem directory for any
	// temporary data created by the repositories provided by the provider.
	TempDir string

	// UserAgent is the User-Agent string to be used in HTTP requests by all the
	// repositories provided by the provider.
	UserAgent string

	// Scheme is the runtime scheme used by the repositories.
	Scheme *runtime.Scheme

	// HTTPClient is the HTTP client for requests to the registry.
	HTTPClient *http.Client

	// Timeout is the timeout for HTTP requests to the registry.
	Timeout time.Duration
}

type Option func(*Options)

// WithTempDir sets the temporary directory option
func WithTempDir(dir string) Option {
	return func(o *Options) {
		o.TempDir = dir
	}
}

func WithScheme(scheme *runtime.Scheme) Option {
	return func(o *Options) {
		o.Scheme = scheme
	}
}

// WithHTTPClient sets the HTTP client option
func WithHTTPClient(client *http.Client) Option {
	return func(o *Options) {
		o.HTTPClient = client
	}
}
