package spec

// UploadType determines how resources are stored in the target repository during transfer.
//
// This option is only relevant when resources are being copied (i.e., when [CopyModeAllResources]
// is set or for local blob resources in the default mode). It controls whether resources are
// embedded as local blobs within the component descriptor or uploaded as separate OCI artifacts
// with their own repository references.
// +ocm:jsonschema-gen:enum=default,localBlob,ociArtifact
type UploadType string

const (
	// UploadAsDefault lets the transfer logic decide the upload strategy based on the source
	// access type and target repository capabilities. Local blob resources remain as local blobs,
	// and the original access semantics are preserved where possible.
	UploadAsDefault UploadType = "default"

	// UploadAsLocalBlob forces all transferred resources to be stored as local blobs
	// in the target repository. The resource content is embedded directly in the
	// component version's OCI manifest layers.
	UploadAsLocalBlob UploadType = "localBlob"

	// UploadAsOciArtifact uploads transferred resources as separate OCI artifacts in the
	// target registry, each with their own repository and tag. The component descriptor's
	// resource access is updated to reference the new OCI image location. This is only
	// supported when the target is an OCI registry (not CTF).
	UploadAsOciArtifact UploadType = "ociArtifact"
)

// AllUploadTypes lists every valid [UploadType] in declaration order.
// CLI/flag builders should drive their enum sets from this slice so a new
// constant added above is picked up without editing call sites.
var AllUploadTypes = []UploadType{UploadAsDefault, UploadAsLocalBlob, UploadAsOciArtifact}
