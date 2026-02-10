package resource

import filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"

type options struct {
	filesystemConfig *filesystemv1alpha1.Config
}

type Option func(*options)

// WithFilesystemConfig sets the filesystem configuration for the resource registry, which will be used by plugins that require filesystem access.
// If not set, temporary directories provided by the OS will be used by default.
func WithFilesystemConfig(cfg *filesystemv1alpha1.Config) Option {
	return func(opts *options) {
		opts.filesystemConfig = cfg
	}
}
