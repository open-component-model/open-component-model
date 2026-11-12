package v1alpha1

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
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

// Duration is a [time.Duration] serialized as a Go duration string such as
// 30s, 10m or 1h30m. Numbers are rejected on input.
//
// +ocm:jsonschema-gen=true
// +ocm:jsonschema-gen:schema-from=schemas/Duration.schema.json
type Duration time.Duration

// NewDuration returns a pointer to d as a [Duration].
func NewDuration(d time.Duration) *Duration {
	v := Duration(d)
	return &v
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration must be a string like 30s, 10m or 1h30m: %w", err)
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// Config configures the OCI caches.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// +ocm:jsonschema-gen:enum=oci.cache.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=oci.cache.config.ocm.software
	Type runtime.Type `json:"type"`

	// Enabled turns the OCI caches on or off. false disables the blob and the reference
	// cache: every read goes to the registry and no cache directory is created.
	// Defaults to true.
	Enabled *bool `json:"enabled,omitempty"`

	// RemotePolicy controls when the caches contact the registry on a cache hit:
	// IfNotPresent (default) or Always. See [RemotePolicy].
	RemotePolicy RemotePolicy `json:"remotePolicy,omitempty"`

	// TTL is the maximum age of an entry in the blob and the reference cache,
	// as a positive duration string such as 30s, 10m or 1h30m. Defaults to 10m.
	TTL *Duration `json:"ttl,omitempty"`

	// MaxBlobSize is the largest blob in bytes the blob cache stores. Larger blobs
	// are fetched as usual but not cached. Must be positive. Defaults to 20971520 (20 MiB).
	// The reference cache ignores it.
	// +ocm:jsonschema-gen:minimum=1
	MaxBlobSize *int64 `json:"maxBlobSize,omitempty"`

	// MaxTotalSize bounds the bytes the blob cache keeps on disk. When it is exceeded, the
	// least recently used blobs are removed. 0 means no limit. Must not be negative.
	// Defaults to no limit in the CLI and to 1073741824 (1 GiB) in the controller.
	// The reference cache ignores it.
	// +ocm:jsonschema-gen:minimum=0
	MaxTotalSize *int64 `json:"maxTotalSize,omitempty"`
}

// IsEnabled reports whether the caches are on. A nil c or an unset Enabled means on.
func (c *Config) IsEnabled() bool {
	return c == nil || c.Enabled == nil || *c.Enabled
}

// Validate rejects a RemotePolicy that is neither empty nor a known policy,
// a TTL or MaxBlobSize that is set but not positive, and a negative MaxTotalSize.
func (c *Config) Validate() error {
	var errs []error
	switch c.RemotePolicy {
	case "", RemotePolicyIfNotPresent, RemotePolicyAlways:
	default:
		errs = append(errs, fmt.Errorf("unknown remotePolicy %q, must be %q or %q",
			c.RemotePolicy, RemotePolicyIfNotPresent, RemotePolicyAlways))
	}
	if c.TTL != nil && *c.TTL <= 0 {
		errs = append(errs, fmt.Errorf("invalid ttl %s, must be positive", time.Duration(*c.TTL)))
	}
	if c.MaxBlobSize != nil && *c.MaxBlobSize <= 0 {
		errs = append(errs, fmt.Errorf("invalid maxBlobSize %d, must be positive", *c.MaxBlobSize))
	}
	if c.MaxTotalSize != nil && *c.MaxTotalSize < 0 {
		errs = append(errs, fmt.Errorf("invalid maxTotalSize %d, must not be negative", *c.MaxTotalSize))
	}
	return errors.Join(errs...)
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
		if c.Enabled != nil {
			merged.Enabled = c.Enabled
		}
		if c.RemotePolicy != "" {
			merged.RemotePolicy = c.RemotePolicy
		}
		if c.TTL != nil {
			merged.TTL = c.TTL
		}
		if c.MaxBlobSize != nil {
			merged.MaxBlobSize = c.MaxBlobSize
		}
		if c.MaxTotalSize != nil {
			merged.MaxTotalSize = c.MaxTotalSize
		}
	}

	return merged
}
