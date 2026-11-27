package cache

import (
	"cmp"
	"time"

	cachev1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/config/cache/v1alpha1"
)

// BlobOptions returns opts with the remote policy and the blob section of cfg applied, or nil if
// the blob cache is off. A nil opts leaves the blob cache off unless cfg enables it. The result
// is a copy, so opts shared between callers are not mutated.
func BlobOptions(opts *Options, cfg *cachev1alpha1.Config) *Options {
	if !cfg.BlobEnabled(opts != nil) {
		return nil
	}
	var applied Options
	if opts != nil {
		applied = *opts
	}
	if cfg == nil {
		return &applied
	}
	applied.RemotePolicy = cmp.Or(RemotePolicy(cfg.RemotePolicy), applied.RemotePolicy)
	if b := cfg.Blob; b != nil {
		if b.TTL != nil {
			applied.TTL = time.Duration(*b.TTL)
		}
		if b.MaxBlobSize != nil {
			applied.MaxBlobSize = *b.MaxBlobSize
		}
		if b.MaxTotalSize != nil {
			applied.MaxTotalSize = *b.MaxTotalSize
			if applied.MaxTotalSize == 0 {
				applied.MaxTotalSize = NoTotalSizeLimit // 0 lifts the limit in the config
			}
		}
	}
	return &applied
}

// ReferenceOptions is [BlobOptions] for the reference section. It also returns nil under
// [RemotePolicyAlways]: such a reference cache would serve nothing, see [ReferenceCache.Resolve].
func ReferenceOptions(opts *Options, cfg *cachev1alpha1.Config) *Options {
	if !cfg.ReferenceEnabled(opts != nil) {
		return nil
	}
	var applied Options
	if opts != nil {
		applied = *opts
	}
	if cfg != nil {
		applied.RemotePolicy = cmp.Or(RemotePolicy(cfg.RemotePolicy), applied.RemotePolicy)
		if r := cfg.Reference; r != nil {
			if r.TTL != nil {
				applied.TTL = time.Duration(*r.TTL)
			}
			if r.MaxEntries != nil {
				applied.MaxEntries = *r.MaxEntries
			}
		}
	}
	if applied.RemotePolicy == RemotePolicyAlways {
		return nil
	}
	return &applied
}
