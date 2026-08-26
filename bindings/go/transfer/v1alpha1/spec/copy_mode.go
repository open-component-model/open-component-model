package spec

// CopyMode determines which resources are copied during a transfer operation.
//
// When building a transformation graph, the CopyMode controls whether only local blob
// resources are included or all resources (including remote OCI artifacts and Helm charts)
// are fetched and re-uploaded to the target repository.
// +ocm:jsonschema-gen:enum=localBlob,allResources
type CopyMode string

const (
	// CopyModeLocalBlobResources is the default copy mode. It transfers only resources
	// that are stored as local blobs within the source repository. Remote references
	// (such as OCI image artifacts or Helm charts hosted externally) are left as-is
	// in the component descriptor - their access specifications are preserved unchanged.
	CopyModeLocalBlobResources CopyMode = "localBlob"

	// CopyModeAllResources transfers all resources regardless of their access type.
	// Remote OCI artifacts are downloaded and re-uploaded to the target, and Helm charts
	// are fetched, converted to OCI format, and stored in the target repository.
	// This mode ensures the target repository is fully self-contained.
	CopyModeAllResources CopyMode = "allResources"
)
