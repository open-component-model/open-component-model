package internal

type CopyMode int

const (
	// CopyModeLocalBlobResources is the default copy mode, which does not download remote references like oci artifacts,
	// but copies local resources.
	CopyModeLocalBlobResources CopyMode = iota
	// CopyModeAllResources copies all resources, including remote references like oci artifacts.
	CopyModeAllResources
)

type Options struct {
	CopyMode  CopyMode
	Recursive bool
}

type Option func(*Options)

// WithCopyMode sets the copy mode for the transfer operation. The copy mode determines which resources are copied during the transfer.
// The default copy mode (CopyModeLocalBlobResources) does not download remote references like OCI artifacts, but copies local resources and component references.
func WithCopyMode(mode CopyMode) func(*Options) {
	return func(o *Options) {
		o.CopyMode = mode
	}
}

func WithRecursive(recursive bool) func(*Options) {
	return func(o *Options) {
		o.Recursive = recursive
	}
}
