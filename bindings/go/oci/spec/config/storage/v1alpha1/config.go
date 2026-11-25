package v1alpha1

import (
	"fmt"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// ConfigType defines the type identifier for OCI storage configurations.
	ConfigType = "oci.storage.config.ocm.software"
)

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{},
		runtime.NewVersionedType(ConfigType, Version),
		runtime.NewUnversionedType(ConfigType),
	)
}

// Config configures how component versions are stored in OCI registries and CTF archives.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// +ocm:jsonschema-gen:enum=oci.storage.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=oci.storage.config.ocm.software
	Type runtime.Type `json:"type"`

	// ComponentVersionLayout selects the storage layout of every component version written to an
	// OCI registry or CTF archive: default (the tag resolves to the manifest holding the full
	// component descriptor) or normalized (experimental; the tag resolves to a stable manifest holding
	// only the normalized descriptor, which can be signed with cosign or notary). Component versions are
	// always rewritten in this layout, including when a signature is added with "ocm sign
	// component-version". Reading detects the layout of each component version, so repositories
	// may contain both.
	// +ocm:jsonschema-gen:enum=default,normalized
	ComponentVersionLayout layout.ComponentVersionLayout `json:"componentVersionLayout,omitempty"`
}

// Validate rejects an unknown ComponentVersionLayout.
func (c *Config) Validate() error {
	if err := c.ComponentVersionLayout.Validate(); err != nil {
		return fmt.Errorf("invalid componentVersionLayout: %w", err)
	}
	return nil
}

// LookupConfig validates every OCI storage configuration in cfg and merges them.
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
			return nil, fmt.Errorf("failed to decode oci storage config: %w", err)
		}
		if err := config.Validate(); err != nil {
			return nil, fmt.Errorf("invalid oci storage config: %w", err)
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
		if c.ComponentVersionLayout != "" {
			merged.ComponentVersionLayout = c.ComponentVersionLayout
		}
	}

	return merged
}
