package spec

import (
	"fmt"
	"log/slog"
	"reflect"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	ConfigType = "ocm.config.ocm.software"
	Version    = "v1"
)

const (
	DefaultLookupPriority = 10
)

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{},
		runtime.NewVersionedType(ConfigType, Version),
		runtime.NewUnversionedType(ConfigType),
	)
}

// Config is the OCM configuration type for configuring legacy fallback
// resolvers.
//
//	type: ocm.config.ocm.software
//	resolvers:
//	- repository:
//	    type: CommonTransportFormat/v1
//	    filePath: ./ocm/primary-transport-archive
//	    priority: 100
//	- repository:
//	    type: CommonTransportFormat/v1
//	    filePath: ./ocm/primary-transport-archive
//	    priority: 10
//
// Deprecated: Resolvers are deprecated and are only added for backwards
// compatibility.
// New concepts will likely be introduced in the future (contributions welcome!).
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// +ocm:jsonschema-gen:enum=ocm.config.ocm.software/v1
	// +ocm:jsonschema-gen:enum:deprecated=ocm.config.ocm.software
	Type runtime.Type `json:"type"`

	//nolint:gocritic // Deprecated field is okay
	// With aliases repository alias names can be mapped to a repository specification.
	// The alias name can be used in a string notation for an OCM repository.
	// Deprecated: Aliases are deprecated and are ignored with a warning message.
	Aliases map[string]*runtime.Raw `json:"aliases,omitempty"`

	// Resolvers define a list of OCM repository specifications to be used to resolve
	// dedicated component versions.
	// All matching entries are tried to lookup a component version in the following
	//    order:
	//    - highest priority first
	//
	// The default priority is spec.DefaultLookupPriority (10).
	//
	// Repositories with a specified prefix are only even tried if the prefix
	// matches the component name.
	//
	// If resolvers are defined, it is possible to use component version names on the
	// command line without a repository. The names are resolved with the specified
	// resolution rule.
	//
	// They are also used as default lookup repositories to lookup component references
	// for recursive operations on component versions («--lookup» option).
	Resolvers []*Resolver `json:"resolvers,omitempty"`
}

// Resolver assigns a priority and a prefix to a single OCM repository specification
// to allow defining a lookup order for component versions.
//
// Deprecated: Resolvers are deprecated and are only added for backwards
// compatibility.
// New concepts will likely be introduced in the future (contributions welcome!).
//
// +k8s:deepcopy-gen=true
type Resolver struct {
	// Repository is the OCM repository specification to be used for resolving
	// component versions.
	Repository *runtime.Raw `json:"repository"`

	// Optionally, a component name prefix can be given.
	// It limits the usage of the repository to resolve only
	// components with the given name prefix (always complete name segments).
	Prefix string `json:"prefix,omitempty"`

	// An optional priority can be used to influence the lookup order. Larger value
	// means higher priority (default DefaultLookupPriority).
	// Pointer because this is optional. To default the priority, we need to be
	// able to distinguish between "not set" and "set to zero".
	// +ocm:jsonschema-gen:minimum=0
	Priority *int `json:"priority,omitempty"`
}

// Lookup creates a new Config from a central V1 config.
//
// Deprecated: Resolvers are deprecated and are only added for backwards
// compatibility.
// New concepts will likely be introduced in the future (contributions welcome!).
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
			return nil, fmt.Errorf("failed to decode credential config: %w", err)
		}
		cfgs = append(cfgs, &config)
	}
	return Merge(cfgs...), nil
}

// Merge merges the provided configs into a single config.
//
// Deprecated: Resolvers are deprecated and are only added for backwards
// compatibility.
// New concepts will likely be introduced in the future (contributions welcome!).
func Merge(configs ...*Config) *Config {
	if len(configs) == 0 {
		return nil
	}

	merged := new(Config)
	merged.Type = configs[0].Type
	merged.Resolvers = make([]*Resolver, 0)
	merged.Aliases = map[string]*runtime.Raw{}

	for _, cfg := range configs {
		// Merge aliases
		for alias, raw := range cfg.Aliases {
			if existing, exists := merged.Aliases[alias]; exists {
				if existing != nil && raw != nil {
					if !reflect.DeepEqual(existing, raw) {
						slog.Info("Two aliases with the same name but different repository specifications."+
							"The new alias will be ignored.", "alias", alias, "existing", existing, "new", raw)
					}
					continue // Skip if the alias is already present with the same value
				}
			}
			merged.Aliases[alias] = raw
		}

		merged.Resolvers = append(merged.Resolvers, cfg.Resolvers...)
	}

	return merged
}
