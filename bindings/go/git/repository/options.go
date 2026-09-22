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
	// CABundle holds PEM certificates added to the system TLS trust roots for
	// HTTPS repositories. Nil uses the system roots alone.
	CABundle []byte
	// HostKeyCallback verifies the host key of SSH repositories. Nil uses the
	// known_hosts files of the current user.
	HostKeyCallback ssh.HostKeyCallback
	// HTTPConfig configures the HTTP client used for http(s) repositories. Nil
	// leaves the current protocol registration unchanged.
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

// WithCABundle adds PEM certificates to the system TLS trust roots.
func WithCABundle(pem []byte) Option {
	return func(o *Options) {
		o.CABundle = append([]byte(nil), pem...)
	}
}

// WithHostKeyCallback overrides SSH verification. The default uses known_hosts.
func WithHostKeyCallback(callback ssh.HostKeyCallback) Option {
	return func(o *Options) {
		o.HostKeyCallback = callback
	}
}

// WithHTTPConfig installs a configured client in go-git's process-global HTTP(S)
// registry: the last configured repository determines the client for all Git
// downloads. Nil leaves the current registration unchanged.
//
// Set CA bundles in cfg, not [WithCABundle]: go-git's per-operation CA option
// requires a plain *http.Transport, whereas the configured client uses a chain.
func WithHTTPConfig(cfg *httpv1alpha1.Config) Option {
	return func(o *Options) {
		o.HTTPConfig = cfg
	}
}
