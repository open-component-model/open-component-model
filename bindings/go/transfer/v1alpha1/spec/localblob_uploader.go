package spec

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// LocalBlobUploaderConfigType is the config type that copies matching resources into
// the target as local blobs of the transferred component version.
const LocalBlobUploaderConfigType = "localblob.uploader.transfer.config.ocm.software"

// DefaultLocalBlobUploaderMatch is the match a [LocalBlobUploaderConfig] uses when none
// is set: every access type the transfer can download.
//
// Writing it explicitly into a config is equivalent to omitting match.
const DefaultLocalBlobUploaderMatch = `resource.access.isType(["LocalBlob", "OCIImage", "OCIImageLayer", "Helm", "Wget", "S3/v2", "GitHub", "Git"])`

func init() {
	Scheme.MustRegisterWithAlias(&LocalBlobUploaderConfig{},
		runtime.NewVersionedType(LocalBlobUploaderConfigType, Version),
		runtime.NewUnversionedType(LocalBlobUploaderConfigType),
	)
}

// LocalBlobUploaderConfig is a declarative rule that downloads the resources it selects
// and embeds them in the target as local blobs of the transferred component version
// (OCI images via GetOCIArtifact, Helm charts converted to OCI, wget, S3, GitHub and Git
// downloads, local blobs as they are). It is carried as an entry inside the central
// generic configuration (generic.config.ocm.software/v1), as a sibling of [Config], and
// extracted with [LookupUploaderConfigs].
//
// Without match it uses [DefaultLocalBlobUploaderMatch]. A selected resource whose
// access type the transfer cannot download fails the transfer.
//
// Declared as the last uploader without a match, it copies every resource no earlier
// uploader selects; this replaces the former `copyMode: allResources` (and is what
// `ocm transfer cv --copy-resources` appends):
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
//	  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type LocalBlobUploaderConfig struct {
	// +ocm:jsonschema-gen:enum=localblob.uploader.transfer.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=localblob.uploader.transfer.config.ocm.software
	Type runtime.Type `json:"type"`

	// Match is a CEL boolean expression selecting the resources this uploader handles. It
	// sees `resource` and `target`; test access types with resource.access.isType. When empty,
	// DefaultLocalBlobUploaderMatch applies; an explicit value replaces it.
	Match string `json:"match,omitempty"`
}

// EffectiveMatch returns the configured match, or [DefaultLocalBlobUploaderMatch]. It implements
// [UploaderConfig].
func (u *LocalBlobUploaderConfig) EffectiveMatch() string {
	return matchOrDefault(u.Match, DefaultLocalBlobUploaderMatch)
}
