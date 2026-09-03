package transfer

import (
	"ocm.software/open-component-model/bindings/go/credentials"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/transfer/internal"
	"ocm.software/open-component-model/bindings/go/transform/graph/builder"
)

// Options holds configuration options for the default transformation builder.
type Options struct {
	// HTTPConfig is the HTTP client configuration (timeouts, per-host overrides)
	// used to build the repositories's internal HTTP client. When nil, default
	// transport timeouts and oras-go's retry behaviour are used.
	// Accepts the serialisable config type so that external plugins can
	// round-trip it over the wire and reconstruct an equivalent client.
	HTTPConfig *httpv1alpha1.Config
}

type Option func(*Options)

// WithHTTPConfig sets the HTTP client configuration used for OCI registry
// traffic. The repository builds its internal client from cfg on construction,
// applying timeouts and per-host overrides. When nil, default transport
// timeouts and oras-go's retry behaviour are used.
func WithHTTPConfig(cfg *httpv1alpha1.Config) Option {
	return func(o *Options) {
		o.HTTPConfig = cfg
	}
}

// NewDefaultBuilder creates a builder.Builder pre-configured with all standard OCI, CTF, and Helm transformers.
// It accepts the repository provider, resource repository, and credential resolver interfaces
// that are needed by the transformers to interact with repositories.
func NewDefaultBuilder(
	repoProvider repository.ComponentVersionRepositoryProvider,
	resourceRepo repository.ResourceRepository,
	credentialProvider credentials.Resolver,
	opts ...Option,
) *builder.Builder {
	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}

	return internal.NewDefaultBuilder(
		repoProvider,
		resourceRepo,
		credentialProvider,
		options.HTTPConfig,
	)
}
