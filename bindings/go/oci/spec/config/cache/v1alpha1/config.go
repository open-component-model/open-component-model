package v1alpha1

import (
	"errors"
	"fmt"
	"time"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	typesv1 "ocm.software/open-component-model/bindings/go/configuration/types/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// ConfigType defines the type identifier for OCI cache configurations.
	ConfigType = "oci.cache.config.ocm.software"
)

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{},
		runtime.NewVersionedType(ConfigType, Version),
		runtime.NewUnversionedType(ConfigType),
	)
}

// RemotePolicy controls when the OCI caches contact the registry on a cache hit.
//
// The zero value means [RemotePolicyIfNotPresent].
// +ocm:jsonschema-gen:enum=IfNotPresent,Always
type RemotePolicy string

const (
	// RemotePolicyIfNotPresent returns cached content without contacting the registry.
	RemotePolicyIfNotPresent RemotePolicy = "IfNotPresent"
	// RemotePolicyAlways asks the registry to authorize every cache hit.
	RemotePolicyAlways RemotePolicy = "Always"
)

// Config configures the OCI caches: the blob cache, which keeps blobs by digest, and the
// reference cache, which keeps resolved tags and digests.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// +ocm:jsonschema-gen:enum=oci.cache.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=oci.cache.config.ocm.software
	Type runtime.Type `json:"type"`

	// RemotePolicy controls when the caches contact the registry on a cache hit:
	// IfNotPresent (default) or Always. See [RemotePolicy].
	RemotePolicy RemotePolicy `json:"remotePolicy,omitempty"`

	// Blob configures the blob cache.
	Blob *BlobCacheConfig `json:"blob,omitempty"`

	// Reference configures the reference cache.
	Reference *ReferenceCacheConfig `json:"reference,omitempty"`
}

// BlobCacheConfig configures the blob cache. Its entries are addressed by digest, so they
// never go stale.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type BlobCacheConfig struct {
	// Enabled turns the blob cache on or off. Defaults to true.
	Enabled *bool `json:"enabled,omitempty"`

	// TTL is the maximum age of an entry. Under the IfNotPresent remote policy, it is how long
	// a cached blob is served without asking the registry. Must be positive. Defaults to 10m.
	// See [typesv1.Duration] for the accepted forms.
	TTL *typesv1.Duration `json:"ttl,omitempty"`

	// MaxBlobSize is the largest blob in bytes the blob cache stores. Larger blobs are fetched
	// as usual but not cached. Must be positive. Defaults to 20971520 (20 MiB).
	// +ocm:jsonschema-gen:minimum=1
	MaxBlobSize *int64 `json:"maxBlobSize,omitempty"`

	// MaxTotalSize bounds the bytes the blob cache keeps on disk. When it is exceeded, the
	// least recently used blobs are removed. 0 means no limit. Must not be negative.
	// Defaults to no limit in the CLI and to 1073741824 (1 GiB) in the controller.
	// +ocm:jsonschema-gen:minimum=0
	MaxTotalSize *int64 `json:"maxTotalSize,omitempty"`
}

// ReferenceCacheConfig configures the reference cache. It maps a tag or digest to the
// descriptor the registry resolved it to, so repeated resolves need no registry request.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type ReferenceCacheConfig struct {
	// Enabled turns the reference cache on or off. Defaults to true in the CLI and to false
	// in the controller.
	Enabled *bool `json:"enabled,omitempty"`

	// TTL is the maximum age of an entry. Under the IfNotPresent remote policy, a tag moved in
	// the registry resolves to its old digest until the entry expires. Must be positive.
	// Defaults to 10m. See [typesv1.Duration] for the accepted forms.
	TTL *typesv1.Duration `json:"ttl,omitempty"`
}

// BlobEnabled reports whether the blob cache is on. def applies when c or the field is unset.
func (c *Config) BlobEnabled(def bool) bool {
	if c == nil || c.Blob == nil || c.Blob.Enabled == nil {
		return def
	}
	return *c.Blob.Enabled
}

// ReferenceEnabled reports whether the reference cache is on. def applies when c or the field
// is unset.
func (c *Config) ReferenceEnabled(def bool) bool {
	if c == nil || c.Reference == nil || c.Reference.Enabled == nil {
		return def
	}
	return *c.Reference.Enabled
}

// Validate rejects a RemotePolicy that is neither empty nor a known policy, a TTL or
// MaxBlobSize that is set but not positive, and a negative MaxTotalSize.
func (c *Config) Validate() error {
	var errs []error
	switch c.RemotePolicy {
	case "", RemotePolicyIfNotPresent, RemotePolicyAlways:
	default:
		errs = append(errs, fmt.Errorf("unknown remotePolicy %q, must be %q or %q",
			c.RemotePolicy, RemotePolicyIfNotPresent, RemotePolicyAlways))
	}
	if b := c.Blob; b != nil {
		errs = append(errs, validateTTL("blob.ttl", b.TTL))
		if b.MaxBlobSize != nil && *b.MaxBlobSize <= 0 {
			errs = append(errs, fmt.Errorf("invalid blob.maxBlobSize %d, must be positive", *b.MaxBlobSize))
		}
		if b.MaxTotalSize != nil && *b.MaxTotalSize < 0 {
			errs = append(errs, fmt.Errorf("invalid blob.maxTotalSize %d, must not be negative", *b.MaxTotalSize))
		}
	}
	if r := c.Reference; r != nil {
		errs = append(errs, validateTTL("reference.ttl", r.TTL))
	}
	return errors.Join(errs...)
}

func validateTTL(field string, ttl *typesv1.Duration) error {
	if ttl != nil && *ttl <= 0 {
		return fmt.Errorf("invalid %s %s, must be positive", field, time.Duration(*ttl))
	}
	return nil
}

// LookupConfig validates every OCI cache configuration in cfg and merges them.
// It returns nil if cfg holds no entry, so callers apply the default themselves.
func LookupConfig(cfg *genericv1.Config) (*Config, error) {
	if cfg == nil {
		return nil, nil
	}
	filtered, err := genericv1.Filter(cfg, &genericv1.FilterOptions{
		ConfigTypes: []runtime.Type{
			runtime.NewVersionedType(ConfigType, Version),
			runtime.NewUnversionedType(ConfigType),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to filter config: %w", err)
	}
	if len(filtered.Configurations) == 0 {
		return nil, nil
	}
	cfgs := make([]*Config, 0, len(filtered.Configurations))
	for _, entry := range filtered.Configurations {
		var config Config
		if err := Scheme.Convert(entry, &config); err != nil {
			return nil, fmt.Errorf("failed to decode oci cache config: %w", err)
		}
		if err := config.Validate(); err != nil {
			return nil, fmt.Errorf("invalid oci cache config: %w", err)
		}
		cfgs = append(cfgs, &config)
	}
	return Merge(cfgs...), nil
}

// Merge merges the provided configs into a single config. The last set value of each field wins.
func Merge(configs ...*Config) *Config {
	merged := new(Config)
	_, _ = Scheme.DefaultType(merged)

	for _, c := range configs {
		if c == nil {
			continue
		}
		if c.RemotePolicy != "" {
			merged.RemotePolicy = c.RemotePolicy
		}
		if b := c.Blob; b != nil {
			if merged.Blob == nil {
				merged.Blob = new(BlobCacheConfig)
			}
			if b.Enabled != nil {
				merged.Blob.Enabled = b.Enabled
			}
			if b.TTL != nil {
				merged.Blob.TTL = b.TTL
			}
			if b.MaxBlobSize != nil {
				merged.Blob.MaxBlobSize = b.MaxBlobSize
			}
			if b.MaxTotalSize != nil {
				merged.Blob.MaxTotalSize = b.MaxTotalSize
			}
		}
		if r := c.Reference; r != nil {
			if merged.Reference == nil {
				merged.Reference = new(ReferenceCacheConfig)
			}
			if r.Enabled != nil {
				merged.Reference.Enabled = r.Enabled
			}
			if r.TTL != nil {
				merged.Reference.TTL = r.TTL
			}
		}
	}

	return merged
}
