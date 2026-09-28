package spec

import (
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// OCIUploaderConfigType is the config type that uploads matching resources as
// separate OCI artifacts instead of embedding them as local blobs.
const OCIUploaderConfigType = "oci.uploader.transfer.config.ocm.software"

// DefaultOCIImageReference is the CEL template an [OCIUploaderConfig] uses when
// ImageReference is empty. It places the artifact in the target registry (baseUrl plus
// subPath) under the name the resource's access gives it:
//   - a local blob's referenceName, verbatim,
//   - a Helm chart's repository path and chart name, tagged with the chart version,
//   - an OCI image's repository and tag, via toOCI().
//
// Writing it explicitly into a config is equivalent to omitting ImageReference.
const DefaultOCIImageReference = `${target.baseUrl
  + (target.subPath == "" ? "" : "/" + target.subPath)
  + "/" + (has(resource.access.referenceName)
    ? resource.access.referenceName
    : has(resource.access.helmChart)
      ? (url(resource.access.helmRepository).path.split("/") + [resource.access.helmChart.split(":")[0]]).filter(s, s != "").join("/")
        + (has(resource.access.version) && resource.access.version != ""
          ? ":" + resource.access.version
          : (resource.access.helmChart.contains(":") ? ":" + resource.access.helmChart.split(":")[1] : ""))
      : resource.access.toOCI().repository
        + (resource.access.toOCI().tag == "" ? "" : ":" + resource.access.toOCI().tag))}`

// DefaultOCIUploaderWhen is the match.when an [OCIUploaderConfig] uses when none is
// set: OCI registry targets only; OCI images, Helm charts, and local blobs that hold an
// OCI manifest and have a referenceName.
//
// Writing it explicitly into a config is equivalent to omitting match.when.
const DefaultOCIUploaderWhen = `target.type == "OCIRepository"
  && (accessType in ["OCIImage", "Helm"]
    || (accessType == "LocalBlob"
      && isOCIManifest(resource.access.mediaType)
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
// The uploader handles the resources its match selects; without match.when it uses
// [DefaultOCIUploaderWhen]. ImageReference is a CEL template; when omitted,
// [DefaultOCIImageReference] is used. The template is evaluated for each selected
// resource while the transfer graph is built. A selected resource the uploader cannot
// upload (an access type other than OCI image, Helm chart or OCI-manifest local blob),
// or whose ImageReference does not evaluate, fails the transfer.
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	  # relocate one image below a custom registry path (declared first: first match wins)
//	  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
//	    match:
//	      name: my-image
//	    imageReference: '${"ghcr.io/mirror/" + resource.access.toOCI().repository + ":" + resource.access.toOCI().tag}'
//	  # upload every other resource the default match.when selects next to the
//	  # component version (imageReference omitted: DefaultOCIImageReference)
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

	// MatchSpec optionally restricts the resources this uploader selects; when omitted,
	// the default match.when (DefaultOCIUploaderWhen) alone selects. It is exposed as the `match` field; the Go field
	// is named MatchSpec so the type can offer a Match method.
	MatchSpec *UploaderMatch `json:"match,omitempty"`

	// ImageReference is the target image reference: a CEL expression wrapped in ${...}
	// (or a plain literal). `resource` is the source resource; its fields are accessed
	// dynamically, so has() tests for fields of any access type, and
	// resource.access.toOCI() splits an OCI image access into host, registry,
	// repository, tag, digest and reference. `accessType` is the canonical access type
	// name. `target` is the transfer target: an OCI registry has type, baseUrl and
	// subPath; a CTF archive has type and filePath. When empty, it defaults to
	// DefaultOCIImageReference.
	ImageReference string `json:"imageReference,omitempty"`
}

// Validate rejects a non-matching [OCIUploaderConfig.Type]. An empty Type is allowed
// so callers constructing a config programmatically (without going through
// [Scheme.Decode]) do not need to set it.
func (u *OCIUploaderConfig) Validate() error {
	if u == nil {
		return nil
	}
	return validateUploaderType(u.Type, OCIUploaderConfigType)
}

// Match reports whether the static match fields select resource. Without a match every
// resource with an access matches. It implements [UploaderConfig].
func (u *OCIUploaderConfig) Match(resource descriptorv2.Resource) bool {
	return u != nil && matchOptional(u.MatchSpec, resource)
}

// MatchWhen returns the configured match.when, or [DefaultOCIUploaderWhen]. It
// implements [UploaderConfig].
func (u *OCIUploaderConfig) MatchWhen() string {
	if u == nil {
		return ""
	}
	return whenOrDefault(u.MatchSpec, DefaultOCIUploaderWhen)
}
