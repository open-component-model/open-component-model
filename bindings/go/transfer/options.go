package transfer

// CopyMode determines which resources are copied during a transfer operation.
//
// When building a transformation graph via [BuildGraphDefinition], the CopyMode controls
// whether only local blob resources are included or all resources (including remote OCI
// artifacts and Helm charts) are fetched and re-uploaded to the target repository.
type CopyMode int

const (
	// CopyModeLocalBlobResources is the default copy mode. It transfers only resources
	// that are stored as local blobs within the source repository. Remote references
	// (such as OCI image artifacts or Helm charts hosted externally) are left as-is
	// in the component descriptor — their access specifications are preserved unchanged.
	CopyModeLocalBlobResources CopyMode = iota

	// CopyModeAllResources transfers all resources regardless of their access type.
	// Remote OCI artifacts are downloaded and re-uploaded to the target, and Helm charts
	// are fetched, converted to OCI format, and stored in the target repository.
	// This mode ensures the target repository is fully self-contained.
	CopyModeAllResources
)

// UploadType determines how resources are stored in the target repository during transfer.
//
// This option is only relevant when resources are being copied (i.e., when [CopyModeAllResources]
// is set or for local blob resources in the default mode). It controls whether resources are
// embedded as local blobs within the component descriptor or uploaded as separate OCI artifacts
// with their own repository references.
type UploadType int

const (
	// UploadAsDefault lets the transfer logic decide the upload strategy based on the source
	// access type and target repository capabilities. Local blob resources remain as local blobs,
	// and the original access semantics are preserved where possible.
	UploadAsDefault UploadType = iota

	// UploadAsLocalBlob forces all transferred resources to be stored as local blobs
	// in the target repository. The resource content is embedded directly in the
	// component version's OCI manifest layers.
	UploadAsLocalBlob

	// UploadAsOciArtifact uploads transferred resources as separate OCI artifacts in the
	// target registry, each with their own repository and tag. The component descriptor's
	// resource access is updated to reference the new OCI image location. This is only
	// supported when the target is an OCI registry (not CTF).
	UploadAsOciArtifact
)

// Options configures the behavior of a transfer operation.
//
// Transfer mappings must be specified via [WithTransfer] or [WithTransfer].
// Each mapping must include a resolver via [FromResolver] or [FromRepository],
// components via [Component], and a target via [ToRepositorySpec].
type Options struct {
	// Recursive enables recursive discovery and transfer of referenced component versions.
	// When true, the transfer follows component references in the descriptor and transfers
	// all transitively referenced components to the target repository. Referenced component
	// digests are verified against the parent's reference digest if present.
	Recursive bool

	// CopyMode controls which resources are included in the transfer.
	// See [CopyModeLocalBlobResources] and [CopyModeAllResources].
	CopyMode CopyMode

	// UploadType controls how resources are stored in the target repository.
	// See [UploadAsDefault], [UploadAsLocalBlob], and [UploadAsOciArtifact].
	UploadType UploadType

	// Mappings defines which components are transferred to which targets.
	Mappings []Mapping
}

// Option is a functional option for configuring transfer operations.
type Option func(*Options)

// WithCopyMode sets the copy mode for the transfer operation.
// See [CopyMode] for available modes.
func WithCopyMode(mode CopyMode) Option {
	return func(o *Options) {
		o.CopyMode = mode
	}
}

// WithRecursive enables or disables recursive transfer of referenced component versions.
// When enabled, the transfer discovers all component references in the source descriptor
// and transfers them (and their transitive references) to the target repository.
func WithRecursive(recursive bool) Option {
	return func(o *Options) {
		o.Recursive = recursive
	}
}

// WithUploadType sets how resources are stored in the target repository.
// See [UploadType] for available strategies.
func WithUploadType(upload UploadType) Option {
	return func(o *Options) {
		o.UploadType = upload
	}
}

// WithTransfer adds a transfer mapping that routes source components to a target repository.
//
//	transfer.WithTransfer(
//	    transfer.Component("ocm.software/frontend", "1.0.0"),
//	    transfer.ToRepositorySpec(targetRepo),
//	    transfer.FromResolver(sourceResolver),
//	)
func WithTransfer(transferOpts ...TransferOption) Option {
	return func(o *Options) {
		m := Mapping{}
		for _, opt := range transferOpts {
			opt(&m)
		}
		o.Mappings = append(o.Mappings, m)
	}
}
