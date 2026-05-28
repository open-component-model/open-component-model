package repository

// AddLocalResourceOptions carries the optional, construction-time directives for
// [LocalResourceRepository.AddLocalResource]. These are behavioral instructions
// for the add operation, deliberately kept out of [descriptor.Resource]: the
// descriptor is the persisted component model, not a place for per-call
// behavior.
type AddLocalResourceOptions struct {
	// CreateOwnershipReferrer requests that the repository create an
	// asset-to-owner ownership referrer (ADR 0016) for the resource being added,
	// pointing the uploaded artifact back at the owning component version. The
	// constructor sets this when a resource opts in via
	// options.ownershipPolicy: Always. Repositories that cannot host referrers
	// (e.g. out-of-process plugins) ignore it. Copying an ownership referrer that
	// already travels inside the resource's layout (transfer) happens regardless
	// of this flag.
	CreateOwnershipReferrer bool
}

// AddLocalResourceOption configures [AddLocalResourceOptions].
type AddLocalResourceOption func(*AddLocalResourceOptions)

// WithOwnershipReferrerCreation requests creation of an ADR-0016 ownership
// referrer for the resource being added via
// [LocalResourceRepository.AddLocalResource]. Its mere presence opts in; when the
// option is not passed, no referrer is created.
func WithOwnershipReferrerCreation() AddLocalResourceOption {
	return func(o *AddLocalResourceOptions) {
		o.CreateOwnershipReferrer = true
	}
}

// ApplyAddLocalResourceOptions resolves the given options into a single
// [AddLocalResourceOptions] value for implementers of
// [LocalResourceRepository.AddLocalResource] to read.
func ApplyAddLocalResourceOptions(opts ...AddLocalResourceOption) AddLocalResourceOptions {
	var o AddLocalResourceOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}
