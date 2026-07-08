package repository

import "net/http"

const (
	// DefaultMaxDownloadSize is the default maximum download size (100 MiB).
	DefaultMaxDownloadSize int64 = 100 * 1024 * 1024
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

// WithMaxDownloadSize sets the maximum number of bytes to read from a response body.
// Defaults to DefaultMaxDownloadSize (100 MiB) when not set.
// Pass 0 to allow unlimited download size (not recommended for untrusted sources).
func WithMaxDownloadSize(size int64) Option {
	return func(o *Options) {
		o.MaxDownloadSize = &size
	}
}
