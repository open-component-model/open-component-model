package v1alpha1

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const ConfigType = "TransferConfiguration"

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{},
		runtime.NewVersionedType(ConfigType, Version),
		runtime.NewUnversionedType(ConfigType),
	)
}

// Config is the canonical wire format for transfer knobs. Downstream consumers
// (CLI, controllers) load it from YAML/JSON and pass it into [transfer.FromConfig],
// so any new transfer setting belongs here first.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// +ocm:jsonschema-gen:enum=TransferConfiguration/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=TransferConfiguration
	Type runtime.Type `json:"type"`

	// Pointer-typed so an explicit "false" in the wire format is distinguishable
	// from "unset", which lets the controller's CRD spec turn recursion off without
	// ambiguity.
	Recursive *bool `json:"recursive,omitempty"`

	CopyMode CopyMode `json:"copyMode,omitempty"`

	UploadType UploadType `json:"uploadType,omitempty"`
}

func (cfg *Config) GetCopyMode() CopyMode {
	if cfg == nil || cfg.CopyMode == "" {
		return CopyModeLocalBlobResources
	}
	return cfg.CopyMode
}

// GetRecursive collapses the *bool tri-state to a plain bool. Use this rather
// than reading [Config.Recursive] directly so callers don't have to think about
// the nil case the wire format needs.
func (cfg *Config) GetRecursive() bool {
	if cfg == nil || cfg.Recursive == nil {
		return false
	}
	return *cfg.Recursive
}

func (cfg *Config) GetUploadType() UploadType {
	if cfg == nil || cfg.UploadType == "" {
		return UploadAsDefault
	}
	return cfg.UploadType
}

// Validate rejects a non-matching [Config.Type] and unknown enum values.
// An empty Type is allowed so callers constructing a Config programmatically
// (without going through [Scheme.Decode]) do not need to set it explicitly.
// Empty enum fields are allowed; consumers must call [Config.GetCopyMode] /
// [Config.GetUploadType] to resolve them.
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
