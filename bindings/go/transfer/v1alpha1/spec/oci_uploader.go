package spec

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// OCIUploaderConfigType is the config type that uploads matching resources as
// separate OCI artifacts instead of embedding them as local blobs.
const OCIUploaderConfigType = "oci.uploader.transfer.config.ocm.software"

// ociTargetPrefix is the CEL expression of the target registry location all default image
// references start with: baseUrl plus subPath, followed by a slash.
const ociTargetPrefix = `target.baseUrl + (target.subPath == "" ? "" : "/" + target.subPath) + "/"`

// The default image references an [OCIUploaderConfig] uses when ImageReference is empty,
// one per access type the uploader can upload; the uploader picks the one matching the
// access of the selected resource. Each places the artifact in the target registry
// (baseUrl plus subPath) under the name the access gives it. Writing the one for an access
// type explicitly into a config is equivalent to omitting ImageReference for it.
const (
	// DefaultOCIImageReferenceOCIImage names an OCI image by the repository and tag of its
	// image reference, via toOCI(): ghcr.io/org/image:v1 becomes <target>/org/image:v1.
	DefaultOCIImageReferenceOCIImage = "${" + ociTargetPrefix + ` + resource.access.toOCI().repository
  + (resource.access.toOCI().tag == "" ? "" : ":" + resource.access.toOCI().tag)}`

	// DefaultOCIImageReferenceHelm names a Helm chart by the path of its repository URL and
	// the chart name, tagged with the chart version: https://charts.example/stable with chart
	// app and version 1.0.0 becomes <target>/stable/app:1.0.0.
	DefaultOCIImageReferenceHelm = "${" + ociTargetPrefix + ` + (url(resource.access.helmRepository).path.split("/") + [resource.access.helmChart.split(":")[0]]).filter(s, s != "").join("/")
  + (has(resource.access.version) && resource.access.version != ""
    ? ":" + resource.access.version
    : (resource.access.helmChart.contains(":") ? ":" + resource.access.helmChart.split(":")[1] : ""))}`

	// DefaultOCIImageReferenceLocalBlob names a local blob holding an OCI manifest by its
	// referenceName, verbatim: org/image:v1 becomes <target>/org/image:v1.
	DefaultOCIImageReferenceLocalBlob = "${" + ociTargetPrefix + ` + resource.access.referenceName}`

	// DefaultOCIImageReferenceRelative names a relativeOciReference by its registry-relative
	// reference, verbatim: org/image:v1 becomes <target>/org/image:v1. The reference is
	// already a repository path; its tag/digest ride along.
	DefaultOCIImageReferenceRelative = "${" + ociTargetPrefix + ` + resource.access.reference}`
)

// DefaultOCIUploaderMatch is the match an [OCIUploaderConfig] uses when none is set: OCI
// registry targets only; OCI images, Helm charts, and local blobs that hold an OCI
// manifest and have a referenceName.
//
// Writing it explicitly into a config is equivalent to omitting match.
const DefaultOCIUploaderMatch = `target.type == "OCIRepository"
  && (resource.access.isType(["OCIImage", "Helm", "relativeOciReference"])
    || (resource.access.isType("LocalBlob")
      && has(resource.access.mediaType) && isOCIManifest(resource.access.mediaType)
      && has(resource.access.referenceName)))`

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
// The uploader handles the resources its match selects; without match it uses
// [DefaultOCIUploaderMatch]. ImageReference is a CEL template; when omitted, the default
// for the access type of the selected resource is used ([DefaultOCIImageReferenceOCIImage],
// [DefaultOCIImageReferenceHelm], [DefaultOCIImageReferenceLocalBlob]). The template is
// evaluated for each selected resource while the transfer graph is built. A selected resource the uploader cannot
// upload (an access type other than OCI image, Helm chart or OCI-manifest local blob),
// or whose ImageReference does not evaluate, fails the transfer.
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	  # relocate one image below a custom registry path (declared first: first match wins)
//	  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
//	    match: resource.name == "my-image"
//	    imageReference: '${"ghcr.io/mirror/" + resource.access.toOCI().repository + ":" + resource.access.toOCI().tag}'
//	  # upload every other resource the default match selects next to the
//	  # component version (imageReference omitted: the per-access-type default)
//	  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type OCIUploaderConfig struct {
	// +ocm:jsonschema-gen:enum=oci.uploader.transfer.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=oci.uploader.transfer.config.ocm.software
	Type runtime.Type `json:"type"`

	// Match is a CEL boolean expression selecting the resources this uploader handles. It
	// sees `resource` and `target`; test access types with resource.access.isType. When empty,
	// DefaultOCIUploaderMatch applies; an explicit value replaces it.
	Match string `json:"match,omitempty"`

	// ImageReference is the target image reference: a CEL expression wrapped in ${...}
	// (or a plain literal). `resource` is the source resource; its fields are accessed
	// dynamically, so has() tests for fields of any access type, and
	// resource.access.toOCI() splits an OCI image access into host, registry,
	// repository, tag, digest and reference; resource.access.isType tests the access
	// type with aliases resolved. `target` is the transfer target: an OCI registry has
	// type, baseUrl and subPath; a CTF archive has type and filePath. When empty, the
	// default for the access type of the selected resource applies
	// (DefaultOCIImageReferenceOCIImage, DefaultOCIImageReferenceHelm,
	// DefaultOCIImageReferenceLocalBlob).
	ImageReference string `json:"imageReference,omitempty"`
}

// EffectiveMatch returns the configured match, or [DefaultOCIUploaderMatch]. It implements
// [UploaderConfig].
func (u *OCIUploaderConfig) EffectiveMatch() string {
	return matchOrDefault(u.Match, DefaultOCIUploaderMatch)
}
