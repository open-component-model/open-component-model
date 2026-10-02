// Package configuration translates the OCI caching wire configuration
// (caching.oci.config.ocm.software) into low-level [cache.Options].
package configuration

import (
	"fmt"
	"time"

	"ocm.software/open-component-model/bindings/go/oci/cache"
	ocicachingv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/config/v1alpha1"
)

// Resolve returns the blob and reference cache options for cfg.
//
// A nil cfg, or an omitted mode, selects fallback as the remote policy.
// fallback must be [cache.RemotePolicyAlways] or [cache.RemotePolicyIfNotPresent].
// Mode Never returns two nil option sets, which the repository provider treats as
// "caches disabled".
//
// Options always start from [cache.Defaults] because the cache package
// replaces zero values with defaults; only explicitly configured values are
// overwritten. The returned option sets never share a pointer. Dir is left
// empty for the provider to derive.
func Resolve(
	cfg *ocicachingv1alpha1.Config,
	fallback cache.RemotePolicy,
) (blobOptions, referenceOptions *cache.Options, err error) {
	switch fallback {
	case cache.RemotePolicyAlways, cache.RemotePolicyIfNotPresent:
	default:
		return nil, nil, fmt.Errorf("invalid fallback remote policy %q", fallback)
	}
	if err := cfg.Validate(); err != nil {
		return nil, nil, fmt.Errorf("invalid OCI caching configuration: %w", err)
	}

	policy := fallback
	if cfg != nil {
		switch cfg.Mode {
		case ocicachingv1alpha1.ModeNever:
			return nil, nil, nil
		case ocicachingv1alpha1.ModeAlways:
			policy = cache.RemotePolicyAlways
		case ocicachingv1alpha1.ModeIfNotPresent:
			policy = cache.RemotePolicyIfNotPresent
		}
	}

	blobOptions, referenceOptions = cache.Defaults(), cache.Defaults()
	blobOptions.RemotePolicy, referenceOptions.RemotePolicy = policy, policy

	if cfg != nil {
		if cfg.TTL != nil {
			blobOptions.TTL = time.Duration(*cfg.TTL)
			referenceOptions.TTL = time.Duration(*cfg.TTL)
		}
		if cfg.MaxBlobSize != nil {
			blobOptions.MaxBlobSize = *cfg.MaxBlobSize
		}
	}
	return blobOptions, referenceOptions, nil
}
