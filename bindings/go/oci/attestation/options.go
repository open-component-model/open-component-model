package attestation

import (
	"runtime"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
)

type options struct {
	platform       ociImageSpecV1.Platform
	allPlatforms   bool
	predicateTypes []string
}

// Option configures SBOM discovery.
type Option func(*options)

// WithAllPlatforms widens the discovery to every platform the index offers, ignoring
// any platform set by WithPlatform.
func WithAllPlatforms() Option {
	return func(o *options) {
		o.allPlatforms = true
	}
}

// WithPlatform narrows the discovery to a platform.
func WithPlatform(platform ociImageSpecV1.Platform) Option {
	return func(o *options) {
		if platform.Architecture != "" {
			o.platform.Architecture = platform.Architecture
		}
		if platform.OS != "" {
			o.platform.OS = platform.OS
		}
		if platform.Variant != "" {
			o.platform.Variant = platform.Variant
		}
		if platform.OSVersion != "" {
			o.platform.OSVersion = platform.OSVersion
		}
		if len(platform.OSFeatures) > 0 {
			o.platform.OSFeatures = platform.OSFeatures
		}
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
		// Do not default OS otherwise, we'll restrict images not on current os.
		platform: ociImageSpecV1.Platform{
			Architecture: runtime.GOARCH,
		},
		predicateTypes: []string{PredicateTypeSPDX},
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}
