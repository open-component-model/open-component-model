package attestation

import (
	"runtime"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
)

type options struct {
	platform       ociImageSpecV1.Platform
	predicateTypes []string
}

// Option configures SBOM discovery.
type Option func(*options)

// WithPlatform adds platform information to the discovery.
func WithPlatform(platform ociImageSpecV1.Platform) Option {
	return func(o *options) {
		o.platform = platform
	}
}

// WithPredicateTypes optionally sets predicate type to discover.
// Default is PredicateTypeSPDX.
func WithPredicateTypes(types ...string) Option {
	return func(o *options) {
		if len(types) == 0 {
			return
		}
		o.predicateTypes = types
	}
}

func newOptions(opts ...Option) *options {
	o := &options{
		platform: ociImageSpecV1.Platform{
			Architecture: runtime.GOARCH,
			OS:           runtime.GOOS,
		},
		predicateTypes: []string{PredicateTypeSPDX},
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}
