package repository

import (
	"golang.org/x/crypto/ssh"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
)

// Options holds configuration for the Git resource repository.
type Options struct {
	// MaxArchiveSize caps compressed output, not the Git transfer.
	// Nil uses 1 GiB; non-positive values disable the limit.
	MaxArchiveSize *int64

	// HostKeyCallback verifies the host key of SSH repositories. Nil uses the
	// known_hosts files of the current user.
	HostKeyCallback ssh.HostKeyCallback
	// HTTPConfig configures the HTTP client used for http(s) repositories. Nil
	// uses the shared OCM client defaults.
	HTTPConfig *httpv1alpha1.Config
}

// Option configures Options.
type Option func(*Options)

// WithMaxArchiveSize caps compressed output bytes, not the preceding Git transfer.
// Non-positive values disable the limit; output is streamed to disk.
func WithMaxArchiveSize(size int64) Option {
	return func(o *Options) {
		o.MaxArchiveSize = &size
	}
}

// WithHostKeyCallback overrides SSH verification. The default uses known_hosts.
func WithHostKeyCallback(callback ssh.HostKeyCallback) Option {
	return func(o *Options) {
		o.HostKeyCallback = callback
	}
}

// WithHTTPConfig configures the OCM HTTP client this repository uses for Git
// sessions. Nil uses the shared OCM client defaults. CA trust uses Go's
// system/environment configuration, not repository options.
func WithHTTPConfig(cfg *httpv1alpha1.Config) Option {
	return func(o *Options) {
		o.HTTPConfig = cfg
	}
}
