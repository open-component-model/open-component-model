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

// Policy controls whether an asset-to-owner OCI referrer (ADR 0016) is pushed
// alongside each by-value resource upload.
type Policy string

const (
	// PolicyDisabled disables asset-to-owner referrer creation. This is the
	// effective policy when no configuration is supplied or the field is
	// omitted.
	PolicyDisabled Policy = "disabled"
	// PolicyAuto pushes one ownership referrer per by-value resource upload.
	// Sources are not annotated.
	PolicyAuto Policy = "auto"
)

// Config is the OCM configuration type that opts resource uploads in to the
// asset-to-owner OCI referrer (ADR 0016).
//
//	type: ownership.config.ocm.software/v1alpha1
//	policy: auto
//	repositories:
//	- repository:
//	    type: OCIRepository/v1
//	  policy: disabled
//	- repository:
//	    type: OCIRepository/v1
//	    baseUrl: ghcr.io
//	    subPath: my-org/components
//	  policy: auto
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// +ocm:jsonschema-gen:enum=ownership.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=ownership.config.ocm.software
	Type runtime.Type `json:"type"`

	// Policy is the default asset-to-owner referrer policy applied to every
	// repository that is not matched by a more specific entry in Repositories.
	// Defaults to Disabled when omitted.
	//
	// +ocm:jsonschema-gen:enum=auto
	// +ocm:jsonschema-gen:enum=disabled
	Policy Policy `json:"policy,omitempty"`

	// Repositories overrides the default Policy for individual OCM
	// repositories. Each entry binds a repository specification to the policy
	// that applies when a resource is uploaded to that repository. Entries are
	// evaluated in order; the first matching entry wins, and the top-level
	// Policy is used as the fallback.
	Repositories []*RepositoryPolicy `json:"repositories,omitempty"`
}

// RepositoryPolicy binds a single OCM repository specification to the
// asset-to-owner referrer policy that applies to uploads targeting it.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type RepositoryPolicy struct {
	// Repository is the OCM repository specification this policy applies to.
	Repository *runtime.Raw `json:"repository"`

	// Policy is the asset-to-owner referrer policy for uploads targeting
	// Repository. Defaults to Disabled when omitted.
	//
	// +ocm:jsonschema-gen:enum=auto
	// +ocm:jsonschema-gen:enum=disabled
	Policy Policy `json:"policy,omitempty"`
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
			return nil, fmt.Errorf("failed to decode ownership config: %w", err)
		}
		cfgs = append(cfgs, &config)
	}
	return Merge(cfgs...), nil
}

// Merge merges the provided configs into a single config. The last non-empty
// top-level Policy wins, and per-repository entries are concatenated in the
// order they are defined.
func Merge(configs ...*Config) *Config {
	if len(configs) == 0 {
		return nil
	}

	merged := new(Config)
	merged.Type = configs[0].Type
	merged.Repositories = make([]*RepositoryPolicy, 0)

	for _, cfg := range configs {
		if cfg.Policy != "" {
			merged.Policy = cfg.Policy
		}
		merged.Repositories = append(merged.Repositories, cfg.Repositories...)
	}

	return merged
}
