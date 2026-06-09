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

	// Recursive configures transferring component references with the parent component.
	// It accepts either an integer or a boolean: -1 means infinite recursion, 0 means
	// no recursion, n > 0 limits recursion to n levels; true is shorthand for -1 and
	// false for 0. See [Recursive].
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

func (cfg *Config) GetCopyMode() CopyMode {
	if cfg == nil || cfg.CopyMode == "" {
		return CopyModeLocalBlobResources
	}
	return cfg.CopyMode
}

// GetRecursive resolves [Config.Recursive] to a plain int depth. Use this
// rather than reading the field directly so callers don't have to handle the
// nil receiver case.
func (cfg *Config) GetRecursive() int {
	if cfg == nil {
		return 0
	}

	return int(cfg.Recursive)
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
