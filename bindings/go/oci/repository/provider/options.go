package provider

import (
	"net/http"

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

	// HTTPClient is the HTTP client used for OCI registry traffic. When nil,
	// the provider falls back to oras-go's retry.DefaultClient. When set, the
	// caller is responsible for any retry/middleware wrapping; the provider
	// uses the client as-is.
	HTTPClient *http.Client
}

type Option func(*Options)

// WithTempDir sets the temporary directory option
func WithTempDir(dir string) Option {
	return func(o *Options) {
		o.TempDir = dir
	}
}

// WithUserAgent sets the user agent option
func WithUserAgent(userAgent string) Option {
	return func(o *Options) {
		o.UserAgent = userAgent
	}
}

func WithScheme(scheme *runtime.Scheme) Option {
	return func(o *Options) {
		o.Scheme = scheme
	}
}

// WithHTTPClient overrides the default HTTP client used for OCI registry
// traffic. When unset, the provider uses oras-go's retry.DefaultClient.
// The caller is responsible for wrapping the client's transport with
// retry/logging/tracing middleware as appropriate.
func WithHTTPClient(client *http.Client) Option {
	return func(o *Options) {
		o.HTTPClient = client
	}
}
