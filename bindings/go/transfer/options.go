package transfer

// CopyMode determines which resources are copied during a transfer operation.
type CopyMode int

const (
	// CopyModeLocalBlobResources is the default copy mode, which does not download remote references like oci artifacts,
	// but copies local resources.
	CopyModeLocalBlobResources CopyMode = iota
	// CopyModeAllResources copies all resources, including remote references like oci artifacts.
	CopyModeAllResources
)

// UploadType determines how resources are uploaded to the target repository.
type UploadType int

const (
	// UploadAsDefault is the default upload mode, which is determined by the source and target repository.
	UploadAsDefault UploadType = iota
	// UploadAsLocalBlob sets the upload of all oci resources as local blobs.
	UploadAsLocalBlob
	// UploadAsOciArtifact sets the upload of all oci resources as OCI artifacts.
	UploadAsOciArtifact
)

// Options configures the behavior of a transfer operation.
type Options struct {
	Recursive  bool
	CopyMode   CopyMode
	UploadType UploadType
}

// Option is a functional option for configuring transfer operations.
type Option func(*Options)

// WithCopyMode sets the copy mode for the transfer operation. The copy mode determines which resources are copied during the transfer.
// The default copy mode (CopyModeLocalBlobResources) does not download remote references like OCI artifacts, but copies local resources and component references.
func WithCopyMode(mode CopyMode) Option {
	return func(o *Options) {
		o.CopyMode = mode
	}
}

// WithRecursive sets whether the transfer operation should recursively discover and transfer component versions.
func WithRecursive(recursive bool) Option {
	return func(o *Options) {
		o.Recursive = recursive
	}
}

// WithUploadType sets the upload type for the transfer operation.
func WithUploadType(upload UploadType) Option {
	return func(o *Options) {
		o.UploadType = upload
	}
}
