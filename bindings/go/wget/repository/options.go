package repository

import (
	"net/http"

	checksumhttpv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/checksum/http/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/wget/internal/download"
)

// DefaultMaxDownloadSize mirrors [download.DefaultMaxDownloadSize]: zero means
// unlimited.
const DefaultMaxDownloadSize int64 = download.DefaultMaxDownloadSize

// Options holds configuration options for the wget resource repository.
type Options struct {
	Client          *http.Client
	MaxDownloadSize *int64
	// WgetConfig steers the digest processor's checksum policy. Nil means
	// "compute SHA-256 without external verification".
	WgetConfig *checksumhttpv1alpha1.Config
}

// Option configures Options.
type Option func(*Options)

// WithHTTPClient sets the HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(o *Options) {
		o.Client = client
	}
}

// WithMaxDownloadSize caps response body bytes. Zero or negative disables the
// limit; bodies are streamed to disk, bounded by free disk rather than RAM.
func WithMaxDownloadSize(size int64) Option {
	return func(o *Options) {
		o.MaxDownloadSize = &size
	}
}

// WithWgetConfig steers the digest processor's checksum policy. Passing the
// same config to both the input method and this option keeps both paths in
// sync.
func WithWgetConfig(cfg *checksumhttpv1alpha1.Config) Option {
	return func(o *Options) {
		o.WgetConfig = cfg
	}
}
