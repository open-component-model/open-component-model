package spec

import (
	"fmt"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// OCIUploaderConfigType is the config type that uploads matching resources as
// separate OCI artifacts instead of embedding them as local blobs.
const OCIUploaderConfigType = "oci.uploader.transfer.config.ocm.software"

// DefaultOCIImageReference is the CEL template an [OCIUploaderConfig] uses when
// ImageReference is empty: the artifact is placed next to the component version in the
// target registry, under the resource's source repository[:tag]. Writing it explicitly
// into a config is equivalent to omitting ImageReference.
const DefaultOCIImageReference = `${targetRepository + "/" + referenceName}`

func init() {
	Scheme.MustRegisterWithAlias(&OCIUploaderConfig{},
		runtime.NewVersionedType(OCIUploaderConfigType, Version),
		runtime.NewUnversionedType(OCIUploaderConfigType),
	)
}

// OCIUploaderConfig is a declarative rule that uploads matching resources as separate
// OCI artifacts during transfer. It is carried as an entry inside the central generic
// configuration (generic.config.ocm.software/v1), as a sibling of [Config], and
// extracted with [LookupUploaderConfigs]. Each entry is an independent rule; entries
// are not merged.
//
// The uploader applies to OCI image and Helm chart resources and to local blobs that
// hold an OCI manifest. ImageReference is a CEL template; when omitted,
// [DefaultOCIImageReference] is used. The uploader applies only if every alias its
// template references is available: `targetRepository` requires an OCI registry target
// and `referenceName` a resource with a reference name. Otherwise the resource falls
// through to the next uploader and finally to the default handling.
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	  # relocate one image below a custom registry path (declared first: first match wins)
//	  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
//	    match:
//	      name: my-image
//	    imageReference: '${"ghcr.io/mirror/" + referenceName}'
//	  # upload every other applicable resource next to the component version
//	  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
//	    imageReference: '${targetRepository + "/" + referenceName}' # the default
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type OCIUploaderConfig struct {
	// +ocm:jsonschema-gen:enum=oci.uploader.transfer.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=oci.uploader.transfer.config.ocm.software
	Type runtime.Type `json:"type"`

	// MatchSpec optionally restricts the resources this uploader applies to; when
	// omitted every resource matches. It is exposed as the `match` field; the Go field
	// is named MatchSpec so the type can offer a Match method.
	MatchSpec *UploaderMatch `json:"match,omitempty"`

	// ImageReference is the target image reference: a CEL expression wrapped in ${...}
	// (or a plain literal) that can use the aliases `resource` (the source resource),
	// `referenceName` (the source repository[:tag]) and `targetRepository` (the target
	// registry base URL including its sub path). When empty, it defaults to
	// ${targetRepository + "/" + referenceName}.
	ImageReference string `json:"imageReference,omitempty"`
}

// Validate rejects a non-matching [OCIUploaderConfig.Type]. An empty Type is allowed
// so callers constructing a config programmatically (without going through
// [Scheme.Decode]) do not need to set it.
func (u *OCIUploaderConfig) Validate() error {
	if u == nil {
		return nil
	}
	if !u.Type.IsEmpty() {
		if u.Type.Name != OCIUploaderConfigType || (u.Type.Version != "" && u.Type.Version != Version) {
			return fmt.Errorf("invalid type %q (must be %q or %q)",
				u.Type, OCIUploaderConfigType, runtime.NewVersionedType(OCIUploaderConfigType, Version))
		}
	}
	return nil
}

// Match reports whether this uploader applies to resource. Without a match every
// resource with an access matches. It implements [UploaderConfig].
func (u *OCIUploaderConfig) Match(resource descriptorv2.Resource) bool {
	if u == nil || resource.Access == nil {
		return false
	}
	if u.MatchSpec == nil {
		return true
	}
	return u.MatchSpec.Matches(resource)
}
