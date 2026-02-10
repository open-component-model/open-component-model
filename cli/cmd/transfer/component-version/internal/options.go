package internal

type CopyMode int

const (
	// CopyModeDefault is the default copy mode, which does not download remote references like oci artifacts,
	// but copies local resources.
	CopyModeDefault CopyMode = iota
	// CopyModeLocalResources only copies all resources with a local relation, including oci artifacts.
	CopyModeLocalResources
	// CopyModeAllResources copies all resources, including remote references like oci artifacts.
	CopyModeAllResources
)

type options struct {
	CopyMode  CopyMode
	Recursive bool
}

type Option func(*options)

// WithCopyMode sets the copy mode for the transfer operation. The copy mode determines which resources are copied during the transfer.
// The default copy mode (CopyModeDefault) does not download remote references like OCI artifacts, but copies local resources and component references.
func WithCopyMode(mode CopyMode) func(*options) {
	return func(o *options) {
		o.CopyMode = mode
	}
}

func WithRecursive(recursive bool) func(*options) {
	return func(o *options) {
		o.Recursive = recursive
	}
}
