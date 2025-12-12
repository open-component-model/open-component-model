package provider

import "ocm.software/open-component-model/bindings/go/runtime"

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
