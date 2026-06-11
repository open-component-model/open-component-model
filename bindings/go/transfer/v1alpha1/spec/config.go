package spec

import (
	"fmt"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const ConfigType = "transfer.config.ocm.software"

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{},
		runtime.NewVersionedType(ConfigType, Version),
		runtime.NewUnversionedType(ConfigType),
	)
}

// Config is the canonical wire format for transfer settings. It is carried as an
// entry inside the central generic configuration
// (generic.config.ocm.software/v1) and extracted with [LookupConfig].
// Downstream consumers (CLI, controllers) pass it directly to
// [transfer.BuildGraphDefinition], so any new transfer setting belongs here first.
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	  - type: transfer.config.ocm.software/v1alpha1
//	    recursive: -1
//	    copyMode: localBlob
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// +ocm:jsonschema-gen:enum=transfer.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=transfer.config.ocm.software
	Type runtime.Type `json:"type"`

	// Recursive configures transferring component references with the parent
	// component: -1 means infinite recursion, 0 means no recursion. Positive
	// depths are reserved but not implemented yet. See [Recursive].
	Recursive Recursive `json:"recursive,omitempty"`

	// CopyMode determines which resources are copied during a transfer operation.
	//
	// When building a transformation graph, the CopyMode controls whether only local blob
	// resources are included or all resources (including remote OCI artifacts and Helm charts)
	// are fetched and re-uploaded to the target repository.
	CopyMode CopyMode `json:"copyMode,omitempty"`

	// UploadType determines how resources are stored in the target repository during transfer.
	//
	// This option is only relevant when resources are being copied (i.e., when [CopyModeAllResources]
	// is set or for local blob resources in the default mode). It controls whether resources are
	// embedded as local blobs within the component descriptor or uploaded as separate OCI artifacts
	// with their own repository references.
	UploadType UploadType `json:"uploadType,omitempty"`
}

// Validate rejects a non-matching [Config.Type] and unknown enum values.
// An empty Type is allowed so callers constructing a Config programmatically
// (without going through [Scheme.Decode]) do not need to set it explicitly.
// Empty enum fields are allowed; consumers resolve them to their defaults
// ([CopyModeLocalBlobResources], [UploadAsDefault]) at the point of use.
func (cfg *Config) Validate() error {
	if cfg == nil {
		return nil
	}

	if !cfg.Type.IsEmpty() {
		if cfg.Type.Name != ConfigType || (cfg.Type.Version != "" && cfg.Type.Version != Version) {
			return fmt.Errorf("invalid type %q (must be %q or %q)",
				cfg.Type, ConfigType, runtime.NewVersionedType(ConfigType, Version))
		}
	}

	if cfg.Recursive < RecursiveInfinite {
		return fmt.Errorf("invalid recursive %d (must be -1 for infinite recursion or 0 for none)", cfg.Recursive)
	}
	if cfg.Recursive > RecursiveNone {
		return fmt.Errorf("recursive depth %d is not implemented yet (use -1 for infinite recursion or 0 for none)", cfg.Recursive)
	}

	switch cfg.CopyMode {
	case "", CopyModeLocalBlobResources, CopyModeAllResources:
	default:
		return fmt.Errorf("invalid copyMode %q (must be one of %q, %q)",
			cfg.CopyMode, CopyModeLocalBlobResources, CopyModeAllResources)
	}
	switch cfg.UploadType {
	case "", UploadAsDefault, UploadAsLocalBlob, UploadAsOciArtifact:
	default:
		return fmt.Errorf("invalid uploadType %q (must be one of %q, %q, %q)",
			cfg.UploadType, UploadAsDefault, UploadAsLocalBlob, UploadAsOciArtifact)
	}
	return nil
}

// LookupConfig extracts the transfer configuration from a central generic
// config. All entries of type [ConfigType] are decoded, validated, and merged
// via [Merge]. Returns nil if cfg is nil or contains no transfer entries.
func LookupConfig(cfg *genericv1.Config) (*Config, error) {
	filtered, err := genericv1.Filter(cfg, &genericv1.FilterOptions{
		ConfigTypes: []runtime.Type{
			runtime.NewVersionedType(ConfigType, Version),
			runtime.NewUnversionedType(ConfigType),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to filter config: %w", err)
	}
	cfgs := make([]*Config, 0, len(filtered.Configurations))
	for _, entry := range filtered.Configurations {
		var config Config
		if err := Scheme.Convert(entry, &config); err != nil {
			return nil, fmt.Errorf("failed to decode transfer config: %w", err)
		}
		if err := config.Validate(); err != nil {
			return nil, fmt.Errorf("invalid transfer config: %w", err)
		}
		cfgs = append(cfgs, &config)
	}
	return Merge(cfgs...), nil
}

// Merge merges the provided configs into a single config. Later entries win:
// a non-empty CopyMode or UploadType and a non-zero Recursive override
// whatever earlier entries set. An explicit "recursive: 0" cannot be
// distinguished from an omitted field; both leave the default of no recursion.
func Merge(configs ...*Config) *Config {
	if len(configs) == 0 {
		return nil
	}

	merged := new(Config)
	merged.Type = runtime.NewVersionedType(ConfigType, Version)
	for _, cfg := range configs {
		if cfg == nil {
			continue
		}
		if cfg.Recursive != RecursiveNone {
			merged.Recursive = cfg.Recursive
		}
		if cfg.CopyMode != "" {
			merged.CopyMode = cfg.CopyMode
		}
		if cfg.UploadType != "" {
			merged.UploadType = cfg.UploadType
		}
	}
	return merged
}
