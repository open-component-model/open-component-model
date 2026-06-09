package provider

import (
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Options holds configuration options for the OCI repository provider.
type Options struct {
	TempDir   string
	UserAgent string
	Scheme    *runtime.Scheme
	HTTPConfig *httpv1alpha1.Config
}

type Option func(*Options)

func WithTempDir(dir string) Option {
	return func(o *Options) { o.TempDir = dir }
}

func WithUserAgent(userAgent string) Option {
	return func(o *Options) { o.UserAgent = userAgent }
}

func WithScheme(scheme *runtime.Scheme) Option {
	return func(o *Options) { o.Scheme = scheme }
}

// WithHTTPConfig sets the HTTP client configuration used for OCI registry traffic.
// The provider builds its internal client from cfg on construction.
// When nil, default transport timeouts and oras-go retry behaviour are used.
func WithHTTPConfig(cfg *httpv1alpha1.Config) Option {
	return func(o *Options) { o.HTTPConfig = cfg }
}
