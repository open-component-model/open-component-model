package provider

import (
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Options holds configuration options for the OCI repository provider.
type Options struct {
	// TempDir is the shared default temporary filesystem directory for any
	// temporary data created by the repositories provided by the provider.
	TempDir string

	// UserAgent identifies the caller and is applied in two distinct ways:
	//   1. Transport-level: injected as the HTTP User-Agent header on every
	//      outbound request via the internal ocmhttp client.
	//   2. Auth-layer: injected as the User-Agent header on authenticated OCI
	//      requests via auth.Client.Header.
	//   3. OCM annotation: written as AnnotationOCMCreator on component versions
	//      added via AddComponentVersion.
	// Defaults to DefaultCreator when empty.
	UserAgent string

	// Scheme is the runtime scheme used by the repositories.
	Scheme *runtime.Scheme

	// HTTPConfig is the HTTP client configuration (timeouts, per-host overrides)
	// used to build the provider's internal HTTP client. When nil, default
	// transport timeouts and oras-go's retry behaviour are used.
	// Accepts the serialisable config type so that external plugins can
	// round-trip it over the wire and reconstruct an equivalent client.
	HTTPConfig *httpv1alpha1.Config
}

type Option func(*Options)

// WithTempDir sets the temporary directory option
func WithTempDir(dir string) Option {
	return func(o *Options) {
		o.TempDir = dir
	}
}

// WithUserAgent sets the caller identity string used in three places: the
// transport-level User-Agent header on every HTTP request, the auth-layer
// User-Agent header on authenticated OCI requests, and the AnnotationOCMCreator
// annotation on newly added component versions. Defaults to DefaultCreator.
func WithUserAgent(userAgent string) Option {
	return func(o *Options) {
		o.UserAgent = userAgent
	}
}

// WithScheme sets the runtime scheme option
func WithScheme(scheme *runtime.Scheme) Option {
	return func(o *Options) {
		o.Scheme = scheme
	}
}

// WithHTTPConfig sets the HTTP client configuration used for OCI registry
// traffic. The provider builds its internal client from cfg on construction,
// applying timeouts and per-host overrides. When nil, default transport
// timeouts and oras-go's retry behaviour are used.
func WithHTTPConfig(cfg *httpv1alpha1.Config) Option {
	return func(o *Options) {
		o.HTTPConfig = cfg
	}
}
