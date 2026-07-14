package repository

import (
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
// Pass 0 to allow unlimited download size (not recommended for untrusted sources).
func WithMaxDownloadSize(size int64) Option {
	return func(o *Options) {
		o.MaxDownloadSize = &size
	}
}
