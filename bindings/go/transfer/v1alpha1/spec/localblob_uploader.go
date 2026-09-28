package spec

import (
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// LocalBlobUploaderConfigType is the config type that copies matching resources into
// the target as local blobs of the transferred component version.
const LocalBlobUploaderConfigType = "localblob.uploader.transfer.config.ocm.software"

// DefaultLocalBlobUploaderWhen is the match.when a [LocalBlobUploaderConfig] uses when
// none is set: every access type the transfer can download.
//
// Writing it explicitly into a config is equivalent to omitting match.when.
const DefaultLocalBlobUploaderWhen = `accessType in ["LocalBlob", "OCIImage", "Helm", "Wget", "S3", "GitHub"]`

func init() {
	Scheme.MustRegisterWithAlias(&LocalBlobUploaderConfig{},
		runtime.NewVersionedType(LocalBlobUploaderConfigType, Version),
		runtime.NewUnversionedType(LocalBlobUploaderConfigType),
	)
}

// LocalBlobUploaderConfig is a declarative rule that downloads the resources it selects
// and embeds them in the target as local blobs of the transferred component version
// (OCI images via GetOCIArtifact, Helm charts converted to OCI, wget, S3 and GitHub
// downloads, local blobs as they are). It is carried as an entry inside the central
// generic configuration (generic.config.ocm.software/v1), as a sibling of [Config], and
// extracted with [LookupUploaderConfigs].
//
// Without match.when it uses [DefaultLocalBlobUploaderWhen]. A selected resource whose
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

	// MatchSpec optionally restricts the resources this uploader selects; when omitted,
	// the default match.when (DefaultLocalBlobUploaderWhen) alone selects. It is exposed
	// as the `match` field; the Go field is named MatchSpec so the type can offer a Match
	// method.
	MatchSpec *UploaderMatch `json:"match,omitempty"`
}

// Validate rejects a non-matching [LocalBlobUploaderConfig.Type]. An empty Type is
// allowed so callers constructing a config programmatically do not need to set it.
func (u *LocalBlobUploaderConfig) Validate() error {
	if u == nil {
		return nil
	}
	return validateUploaderType(u.Type, LocalBlobUploaderConfigType)
}

// Match reports whether the static match fields select resource. Without a match every
// resource with an access matches. It implements [UploaderConfig].
func (u *LocalBlobUploaderConfig) Match(resource descriptorv2.Resource) bool {
	return u != nil && matchOptional(u.MatchSpec, resource)
}

// MatchWhen returns the configured match.when, or [DefaultLocalBlobUploaderWhen]. It
// implements [UploaderConfig].
func (u *LocalBlobUploaderConfig) MatchWhen() string {
	if u == nil {
		return ""
	}
	return whenOrDefault(u.MatchSpec, DefaultLocalBlobUploaderWhen)
}
