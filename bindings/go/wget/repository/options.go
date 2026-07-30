package repository

import (
	"net/http"

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
