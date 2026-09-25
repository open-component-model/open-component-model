package spec

// UploadType determines how resources are stored in the target repository during transfer.
//
// This option is only relevant when resources are being copied (i.e., when [CopyModeAllResources]
// is set or for local blob resources with [CopyModeLocalBlobResources]). It controls whether
// resources are embedded as local blobs within the component descriptor or uploaded as separate
// OCI artifacts with their own repository references. When unset, callers resolve to
// [UploadAsLocalBlob].
// +ocm:jsonschema-gen:enum=localBlob,ociArtifact
type UploadType string

const (
	// UploadAsLocalBlob stores all transferred resources as local blobs in the target
	// repository. The resource content is embedded directly in the component version's
	// OCI manifest layers. This is the default when [UploadType] is unset.
	UploadAsLocalBlob UploadType = "localBlob"

	// UploadAsOciArtifact uploads transferred resources as separate OCI artifacts in the
	// target registry, each with their own repository and tag. The component descriptor's
	// resource access is updated to reference the new OCI image location. This is only
	// supported when the target is an OCI registry (not CTF).
	UploadAsOciArtifact UploadType = "ociArtifact"
)

// AllUploadTypes lists every valid [UploadType] in declaration order.
// CLI/flag builders should drive their enum sets from this slice so a new
// constant added above is picked up without editing call sites. The first
// element is treated as the default by CLI flag builders.
var AllUploadTypes = []UploadType{UploadAsLocalBlob, UploadAsOciArtifact}
