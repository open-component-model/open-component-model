package repository

import (
	"golang.org/x/crypto/ssh"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
)

// Options holds configuration for the Git resource repository.
type Options struct {
	// MaxArchiveSize caps the bytes of the tar archive, not of the clone it is taken
	// from. Nil uses the default; zero or negative allows an unlimited archive,
	// which is then bounded by free disk space.
	MaxArchiveSize *int64
	// CABundle holds PEM certificates added to the system TLS trust roots for
	// HTTPS repositories. Nil uses the system roots alone.
	CABundle []byte
	// HostKeyCallback verifies the host key of SSH repositories. Nil uses the
	// known_hosts files of the current user.
	HostKeyCallback ssh.HostKeyCallback
	// HTTPConfig configures the HTTP client used for http(s) repositories. Nil
	// leaves go-git's default client in place.
	HTTPConfig *httpv1alpha1.Config
}

// Option configures Options.
type Option func(*Options)

// WithMaxArchiveSize limits the bytes of the tar archive a single download
// produces. Pass 0 to allow an unlimited archive. Archives are streamed to disk
// rather than buffered, so an unlimited archive is bounded by free disk space.
// Git transfers the repository before the archive exists, so the limit rejects an
// oversized archive rather than stopping the clone that produced it.
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

// WithHTTPConfig sets the HTTP client configuration used for http(s) repositories.
// Accepts the serialisable config type so that external plugins can round-trip it
// over the wire and reconstruct an equivalent client.
//
// go-git takes no HTTP client per clone or fetch, so the client built from cfg is
// installed into go-git's protocol registry, which is process global: the last
// repository constructed with this option decides the client for every Git
// download in the process. Transports other than http(s) are untouched, and a nil
// cfg leaves go-git's default client in place.
//
// The installed client's transport is a chain rather than a plain *http.Transport,
// which go-git requires when a per-operation CA bundle is set, so a CA bundle for
// an https repository belongs in cfg rather than in [WithCABundle].
func WithHTTPConfig(cfg *httpv1alpha1.Config) Option {
	return func(o *Options) {
		o.HTTPConfig = cfg
	}
}
