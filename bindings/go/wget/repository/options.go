package repository

import (
	"net/http"

	"ocm.software/open-component-model/bindings/go/wget/internal/download"
	wgetconfigv1alpha1 "ocm.software/open-component-model/bindings/go/wget/spec/config/v1alpha1"
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
	// WgetConfig steers wget behavioural knobs — the defaultChecksumPolicy
	// applied on every ProcessResourceDigest call, and per-host overrides
	// thereof. When nil, the digest processor computes SHA-256 without
	// external verification (the pre-config behaviour).
	WgetConfig *wgetconfigv1alpha1.Config
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

// WithWgetConfig steers wget behavioural knobs on the digest processor, most
// notably the defaultChecksumPolicy that is applied to every ProcessResourceDigest
// call. Passing the same config object into the input method
// [wget/input.InputMethod.WgetConfig] and this option keeps both paths in sync.
func WithWgetConfig(cfg *wgetconfigv1alpha1.Config) Option {
	return func(o *Options) {
		o.WgetConfig = cfg
	}
}
