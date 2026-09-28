package repository

import (
	"net/http"

	checksumhttpv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/checksum/http/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/wget/internal/download"
)

const (
	// DefaultMaxDownloadSize is the default maximum download size. Zero means
	// unlimited; see [download.DefaultMaxDownloadSize].
	DefaultMaxDownloadSize int64 = download.DefaultMaxDownloadSize
)

// Options holds configuration options for the wget resource repository.
type Options struct {
	Client          *http.Client
	MaxDownloadSize *int64
	// ChecksumConfig steers the digest processor's checksum mode. Nil means
	// "compute SHA-256 without external verification".
	ChecksumConfig *checksumhttpv1alpha1.Config
}

// Option is a function that configures Options.
type Option func(*Options)

// WithHTTPClient sets the HTTP client to use for requests.
func WithHTTPClient(client *http.Client) Option {
	return func(o *Options) {
		o.Client = client
	}
}

// WithMaxDownloadSize caps the number of bytes read from a response body.
// Zero or negative (the default) means unlimited: bodies are streamed to disk
// rather than held in memory, so a download is bounded by free disk rather than
// by RAM.
func WithMaxDownloadSize(size int64) Option {
	return func(o *Options) {
		o.MaxDownloadSize = &size
	}
}

// WithChecksumConfig steers the digest processor's checksum mode. Passing the
// same config to both the input method and this option keeps both paths in
// sync.
func WithChecksumConfig(cfg *checksumhttpv1alpha1.Config) Option {
	return func(o *Options) {
		o.ChecksumConfig = cfg
	}
}
