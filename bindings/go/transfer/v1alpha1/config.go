package v1alpha1

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const ConfigType = "TransferConfiguration"

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{},
		runtime.NewUnversionedType(ConfigType),
		runtime.NewVersionedType(ConfigType, Version),
	)
}

// Config defines configuration for a component-version transfer operation.
//
// It is the canonical, wire-format representation of the high-level transfer knobs
// (recursive descent, resource copy mode, upload strategy). The CLI loads it via
// --transfer-config and the replication controller consumes the same shape, so any
// new transfer setting belongs here first.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// Type identifies this configuration object's runtime type.
	// +ocm:jsonschema-gen:enum=TransferConfiguration/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=TransferConfiguration
	Type runtime.Type `json:"type"`

	// Recursive enables recursive discovery and transfer of referenced component versions.
	Recursive bool `json:"recursive,omitempty"`

	// CopyMode controls which resources are included in the transfer.
	// See [CopyModeLocalBlobResources] (default) and [CopyModeAllResources].
	CopyMode CopyMode `json:"copyMode,omitempty"`

	// UploadType controls how resources are stored in the target repository.
	// See [UploadAsDefault], [UploadAsLocalBlob], and [UploadAsOciArtifact].
	UploadType UploadType `json:"uploadType,omitempty"`
}

// GetCopyMode returns the configured copy mode, falling back to [CopyModeLocalBlobResources]
// when the field is empty (or the receiver is nil).
func (cfg *Config) GetCopyMode() CopyMode {
	if cfg == nil || cfg.CopyMode == "" {
		return CopyModeLocalBlobResources
	}
	return cfg.CopyMode
}

// GetUploadType returns the configured upload type, falling back to [UploadAsDefault]
// when the field is empty (or the receiver is nil).
func (cfg *Config) GetUploadType() UploadType {
	if cfg == nil || cfg.UploadType == "" {
		return UploadAsDefault
	}
	return cfg.UploadType
}

// Validate rejects a non-matching [Config.Type] and unknown enum values.
// Empty fields are allowed; consumers must call [Config.GetCopyMode] /
// [Config.GetUploadType] (or equivalent) to resolve empties to their
// canonical defaults before acting on the values. An empty Type is also
// allowed so callers constructing a Config programmatically (without going
// through [Scheme.Decode]) do not need to set it explicitly.
func (cfg *Config) Validate() error {
	if cfg == nil {
		return nil
	}

	if !cfg.Type.IsEmpty() && cfg.Type.Name != ConfigType {
		return fmt.Errorf("invalid type %q (must be %q)", cfg.Type, ConfigType)
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
