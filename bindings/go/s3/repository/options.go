package repository

import (
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/s3/internal/download"
)

// Options holds configuration for the S3 resource repository.
type Options struct {
	// Client optionally injects a pre-built S3 client (or a fake, in tests). When
	// nil, a client is constructed per download from the access spec and credentials.
	Client download.ObjectGetter
	// MaxDownloadSize caps the number of bytes read from an object. Nil uses the
	// default (unlimited).
	MaxDownloadSize *int64
	// HTTPConfig configures the HTTP client used to reach S3. Nil uses the
	// shared client's defaults.
	HTTPConfig *httpv1alpha1.Config
}

// Option configures Options.
type Option func(*Options)

// WithClient sets a pre-built S3 client used for all downloads.
func WithClient(client download.ObjectGetter) Option {
	return func(o *Options) {
		o.Client = client
	}
}

// WithMaxDownloadSize sets the maximum number of bytes to read from an object.
// Pass 0 to allow unlimited download size. Object bodies are streamed to disk
// rather than buffered, so an unlimited download is bounded by free disk space.
func WithMaxDownloadSize(size int64) Option {
	return func(o *Options) {
		o.MaxDownloadSize = &size
	}
}

// WithHTTPConfig sets the HTTP client configuration used for object downloads.
// The repository builds its client from cfg on each download. Accepts the
// serialisable config type so that external plugins can round-trip it over the
// wire and reconstruct an equivalent client.
func WithHTTPConfig(cfg *httpv1alpha1.Config) Option {
	return func(o *Options) {
		o.HTTPConfig = cfg
	}
}
