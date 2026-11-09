package v1alpha1

import (
	"fmt"

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

	// RemotePolicy controls when the caches contact the registry on a cache hit:
	// IfNotPresent (default) or Always. See [RemotePolicy].
	RemotePolicy RemotePolicy `json:"remotePolicy,omitempty"`
}

// Validate rejects a RemotePolicy that is neither empty nor a known policy.
func (c *Config) Validate() error {
	switch c.RemotePolicy {
	case "", RemotePolicyIfNotPresent, RemotePolicyAlways:
		return nil
	default:
		return fmt.Errorf("unknown remotePolicy %q, must be %q or %q",
			c.RemotePolicy, RemotePolicyIfNotPresent, RemotePolicyAlways)
	}
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

// Merge merges the provided configs into a single config. The last non-empty value wins.
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
	}

	return merged
}
