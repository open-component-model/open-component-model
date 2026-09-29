package spec

import (
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// ReferenceUploaderConfigType is the config type that keeps matching resources by
// reference: they are not copied and keep their access unchanged in the target.
const ReferenceUploaderConfigType = "reference.uploader.transfer.config.ocm.software"

// DefaultReferenceUploaderMatch is the match a [ReferenceUploaderConfig] uses when none
// is set: every resource except local blobs, which live in the source repository and
// cannot be referenced from the target.
//
// Writing it explicitly into a config is equivalent to omitting match.
const DefaultReferenceUploaderMatch = `!resource.access.isType("LocalBlob")`

func init() {
	Scheme.MustRegisterWithAlias(&ReferenceUploaderConfig{},
		runtime.NewVersionedType(ReferenceUploaderConfigType, Version),
		runtime.NewUnversionedType(ReferenceUploaderConfigType),
	)
}

// ReferenceUploaderConfig is a declarative rule that keeps the resources it selects by
// reference: no transformation is emitted for them and their access is unchanged in the
// target. It is carried as an entry inside the central generic configuration
// (generic.config.ocm.software/v1), as a sibling of [Config], and extracted with
// [LookupUploaderConfigs].
//
// Without match it uses [DefaultReferenceUploaderMatch]. Selecting a local blob
// fails the transfer. Declared before a catch-all, it excludes resources from being
// copied:
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	  # keep the large base image by reference
//	  - type: reference.uploader.transfer.config.ocm.software/v1alpha1
//	    match: resource.name == "base-os-image"
//	  # copy everything else
//	  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type ReferenceUploaderConfig struct {
	// +ocm:jsonschema-gen:enum=reference.uploader.transfer.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=reference.uploader.transfer.config.ocm.software
	Type runtime.Type `json:"type"`

	// Match is a CEL boolean expression selecting the resources this uploader handles. It
	// sees `resource` and `target`; test access types with resource.access.isType. When empty,
	// DefaultReferenceUploaderMatch applies; an explicit value replaces it.
	Match string `json:"match,omitempty"`
}

// Validate rejects a non-matching [ReferenceUploaderConfig.Type]. An empty Type is
// allowed so callers constructing a config programmatically do not need to set it.
func (u *ReferenceUploaderConfig) Validate() error {
	if u == nil {
		return nil
	}
	return validateUploaderType(u.Type, ReferenceUploaderConfigType)
}

// EffectiveMatch returns the configured match, or [DefaultReferenceUploaderMatch]. It implements
// [UploaderConfig].
func (u *ReferenceUploaderConfig) EffectiveMatch() string {
	if u == nil {
		return ""
	}
	if strings.TrimSpace(u.Match) != "" {
		return u.Match
	}
	return DefaultReferenceUploaderMatch
}
