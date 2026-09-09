package repository

import "golang.org/x/crypto/ssh"

type Option func(*ResourceRepository)

// WithTempDir selects the directory for temporary Git objects and archives.
func WithTempDir(dir string) Option {
	return func(r *ResourceRepository) {
		r.options.TempDir = dir
	}
}

// WithMaxDownloadSize limits compressed archive bytes. Zero or negative disables the limit.
func WithMaxDownloadSize(size int64) Option {
	return func(r *ResourceRepository) {
		r.options.MaxDownloadSize = size
	}
}

// WithCABundle adds PEM certificates to the system TLS trust roots.
func WithCABundle(pem []byte) Option {
	return func(r *ResourceRepository) {
		r.options.CABundle = append([]byte(nil), pem...)
	}
}

// WithHostKeyCallback overrides SSH verification. The default uses known_hosts.
func WithHostKeyCallback(callback ssh.HostKeyCallback) Option {
	return func(r *ResourceRepository) {
		r.options.HostKeyCallback = callback
	}
}
