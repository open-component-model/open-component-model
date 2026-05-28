package spec

import (
	"fmt"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	ConfigType = "ownership.config.ocm.software"
	Version    = "v1alpha1"
)

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{},
		runtime.NewVersionedType(ConfigType, Version),
		runtime.NewUnversionedType(ConfigType),
	)
}

// Policy decides whether a resource upload also records the owning
// component version, so a consumer can trace a stored blob back to its
// component (ADR 0016).
type Policy string

const (
	// PolicyNever never writes ownership referrers. This is the behavior
	// when no policy is set.
	PolicyNever Policy = "Never"
	// PolicyAddIfSupported writes an ownership referrer for every resource
	// when the target registry supports referrers.
	PolicyAddIfSupported Policy = "AddIfSupported"
)

// Config enables ownership referrers for resource uploads. The top-level
// Policy sets the default, and Repositories can override it for individual
// repositories.
//
//	type: ownership.config.ocm.software/v1alpha1
//	policy: AddIfSupported
//	repositories:
//	- repository:
//	    type: OCIRepository/v1
//	  policy: Never
//	- repository:
//	    type: OCIRepository/v1
//	    baseUrl: ghcr.io
//	    subPath: my-org/components
//	  policy: AddIfSupported
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// +ocm:jsonschema-gen:enum=ownership.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=ownership.config.ocm.software
	Type runtime.Type `json:"type"`

	// Policy is the default used for any repository without a matching entry
	// in Repositories. When omitted, it behaves as Never.
	//
	// Experimental: ownership referrers are a new feature (ADR 0016) and the
	// policy shape may change or be deprecated in the future.
	//
	// +ocm:jsonschema-gen:enum=AddIfSupported
	// +ocm:jsonschema-gen:enum=Never
	Policy Policy `json:"policy,omitempty"`

	// Repositories overrides Policy for specific repositories.
	Repositories []*RepositoryPolicy `json:"repositories,omitempty"`
}

// RepositoryPolicy pairs a repository with the Policy to use for uploads to
// it.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type RepositoryPolicy struct {
	// Repository is the repository this entry applies to.
	Repository *runtime.Raw `json:"repository"`

	// Policy is used for uploads to Repository. When omitted, it behaves as
	// Never.
	//
	// +ocm:jsonschema-gen:enum=AddIfSupported
	// +ocm:jsonschema-gen:enum=Never
	Policy Policy `json:"policy,omitempty"`
}

// Lookup finds every ownership config entry in cfg and merges them into one
// Config. It returns nil when cfg is nil or has no ownership entries.
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
			return nil, fmt.Errorf("failed to decode ownership config: %w", err)
		}
		cfgs = append(cfgs, &config)
	}
	return Merge(cfgs...), nil
}

// Merge combines several configs into one, or returns nil when none are given.
// Nil configs are skipped: the Type comes from the first non-nil config, the
// Policy from the last non-nil config that sets one, and all repository
// entries are appended in order.
func Merge(configs ...*Config) *Config {
	if len(configs) == 0 {
		return nil
	}

	merged := new(Config)
	merged.Repositories = make([]*RepositoryPolicy, 0)

	typeSet := false
	for _, cfg := range configs {
		if cfg == nil {
			continue
		}
		if !typeSet {
			merged.Type = cfg.Type
			typeSet = true
		}
		if cfg.Policy != "" {
			merged.Policy = cfg.Policy
		}
		merged.Repositories = append(merged.Repositories, cfg.Repositories...)
	}

	return merged
}
