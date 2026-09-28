package spec

import (
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ReferenceUploaderConfigType is the config type that keeps matching resources by
// reference: they are not copied and keep their access unchanged in the target.
const ReferenceUploaderConfigType = "reference.uploader.transfer.config.ocm.software"

// DefaultReferenceUploaderWhen is the match.when a [ReferenceUploaderConfig] uses when
// none is set: every resource except local blobs, which live in the source repository
// and cannot be referenced from the target.
//
// Writing it explicitly into a config is equivalent to omitting match.when.
const DefaultReferenceUploaderWhen = `accessType != "LocalBlob"`

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
// Without match.when it uses [DefaultReferenceUploaderWhen]. Selecting a local blob
// fails the transfer. Declared before a catch-all, it excludes resources from being
// copied:
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	  # keep the large base image by reference
//	  - type: reference.uploader.transfer.config.ocm.software/v1alpha1
//	    match:
//	      name: base-os-image
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

	// MatchSpec optionally restricts the resources this uploader selects; when omitted,
	// the default match.when (DefaultReferenceUploaderWhen) alone selects. It is exposed
	// as the `match` field; the Go field is named MatchSpec so the type can offer a Match
	// method.
	MatchSpec *UploaderMatch `json:"match,omitempty"`
}

// Validate rejects a non-matching [ReferenceUploaderConfig.Type]. An empty Type is
// allowed so callers constructing a config programmatically do not need to set it.
func (u *ReferenceUploaderConfig) Validate() error {
	if u == nil {
		return nil
	}
	return validateUploaderType(u.Type, ReferenceUploaderConfigType)
}

// Match reports whether the static match fields select resource. Without a match every
// resource with an access matches. It implements [UploaderConfig].
func (u *ReferenceUploaderConfig) Match(resource descriptorv2.Resource) bool {
	return u != nil && matchOptional(u.MatchSpec, resource)
}

// MatchWhen returns the configured match.when, or [DefaultReferenceUploaderWhen]. It
// implements [UploaderConfig].
func (u *ReferenceUploaderConfig) MatchWhen() string {
	if u == nil {
		return ""
	}
	return whenOrDefault(u.MatchSpec, DefaultReferenceUploaderWhen)
}
