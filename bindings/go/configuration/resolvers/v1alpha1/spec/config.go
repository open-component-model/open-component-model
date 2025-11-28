package spec

import (
	"fmt"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	ConfigType = "resolvers.config.ocm.software"
	Version    = "v1alpha1"
)

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{},
		runtime.NewVersionedType(ConfigType, Version),
		runtime.NewUnversionedType(ConfigType),
	)
}

// Config is the new OCM configuration type for configuring glob based
// resolvers that replace the deprecated fallback resolvers.
//
//	type: resolvers.config.ocm.software
//	resolvers:
//	- repository:
//	    type: OCIRegistry
//	    baseUrl: ghcr.io
//	    subPath: open-component-model/components
//	    componentName: ocm.software/core/*
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// +ocm:jsonschema-gen:enum=resolvers.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=resolvers.config.ocm.software
	Type runtime.Type `json:"type"`

	// Resolvers define a list of OCM repository specifications to be used to resolve
	// dedicated component versions using glob patterns.
	// All matching entries are tried to lookup a component version in the order
	// they are defined in the configuration.
	//
	// Repositories with a specified componentName pattern are only tried if the pattern
	// matches the component name using glob syntax.
	//
	// If resolvers are defined, it is possible to use component version names on the
	// command line without a repository. The names are resolved with the specified
	// resolution rule.
	//
	// They are also used as default lookup repositories to lookup component references
	// for recursive operations on component versions («--lookup» option).
	Resolvers []*Resolver `json:"resolvers,omitempty"`
}

// Resolver assigns a component name pattern to a single OCM repository specification
// to allow defining component version resolution using glob patterns.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type Resolver struct {
	// Repository is the OCM repository specification to be used for resolving
	// component versions.
	Repository *runtime.Raw `json:"repository"`

	// ComponentNamePattern specifies a glob pattern for matching component names.
	// It limits the usage of the repository to resolve only components with names
	// that match the given pattern.
	// Examples:
	//   - "ocm.software/core/*" (matches any component in the core namespace)
	//   - "*.software/*/test" (matches test components in any software namespace)
	//   - "ocm.software/core/[tc]est" (matches "test" or "cest" in core namespace)
	ComponentNamePattern string `json:"componentNamePattern,omitempty"`
}

// Lookup creates a new Config from a central V1 config.
func Lookup(cfg *genericv1.Config) (*Config, error) {
	if cfg == nil {
		return nil, nil
	}
	cfg, err := genericv1.Filter(cfg, &genericv1.FilterOptions{
		ConfigTypes: []runtime.Type{
			runtime.NewVersionedType(ConfigType, Version),
			runtime.NewUnversionedType(ConfigType),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to filter config: %w", err)
	}
	cfgs := make([]*Config, 0, len(cfg.Configurations))
	for _, entry := range cfg.Configurations {
		var config Config
		if err := Scheme.Convert(entry, &config); err != nil {
			return nil, fmt.Errorf("failed to decode resolver config: %w", err)
		}
		cfgs = append(cfgs, &config)
	}
	return Merge(cfgs...), nil
}

// Merge merges the provided configs into a single config.
func Merge(configs ...*Config) *Config {
	if len(configs) == 0 {
		return nil
	}

	merged := new(Config)
	merged.Type = configs[0].Type
	merged.Resolvers = make([]*Resolver, 0)

	for _, cfg := range configs {
		merged.Resolvers = append(merged.Resolvers, cfg.Resolvers...)
	}

	return merged
}
