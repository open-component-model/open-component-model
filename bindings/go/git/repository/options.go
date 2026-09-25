package repository

import (
	"net/http"

	"golang.org/x/crypto/ssh"
)

// Options holds configuration for the Git resource repository.
type Options struct {
	// MaxArchiveSize caps compressed output, not the Git transfer.
	// Nil, zero and negative values disable the limit.
	MaxArchiveSize *int64

	// HostKeyCallback verifies the host key of SSH repositories. Nil uses the
	// known_hosts files of the current user.
	HostKeyCallback ssh.HostKeyCallback
	// HTTPClient serves http(s) repositories. Nil uses the shared OCM client
	// defaults.
	HTTPClient *http.Client
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

// WithHTTPClient sets the HTTP client this repository uses for Git sessions.
// Nil uses the shared OCM client defaults.
func WithHTTPClient(client *http.Client) Option {
	return func(o *Options) {
		o.HTTPClient = client
	}
}
