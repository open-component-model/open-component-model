package repository

import (
	"net/http"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/npm/internal/download"
)

const (
	// DefaultMaxDownloadSize is the default maximum tarball size. Zero means
	// unlimited; see [download.DefaultMaxDownloadSize].
	DefaultMaxDownloadSize int64 = download.DefaultMaxDownloadSize
)

// Options holds configuration options for the npm resource repository.
type Options struct {
	// Client is the HTTP client registry requests are sent through. Nil builds one
	// from HTTPConfig.
	Client *http.Client
	// HTTPConfig configures the HTTP client used to reach the registry. Nil uses
	// the shared client's defaults.
	HTTPConfig *httpv1alpha1.Config
	// MaxDownloadSize caps the number of bytes read from a tarball. Nil uses the
	// default (unlimited).
	MaxDownloadSize *int64
}

// Option is a function that configures Options.
type Option func(*Options)

// WithHTTPConfig sets the HTTP client configuration used for registry requests.
// The repository builds its client from cfg. Accepts the serialisable config type
// so that external plugins can round-trip it over the wire and reconstruct an
// equivalent client. Its settings are ignored when [WithHTTPClient] is used.
func WithHTTPConfig(cfg *httpv1alpha1.Config) Option {
	return func(o *Options) {
		o.HTTPConfig = cfg
	}
}

// WithHTTPClient sets the HTTP client used for registry requests.
//
// It is used exactly as given, so the settings of [WithHTTPConfig] do not apply
// to it. Build one with ocmhttp.New to get the shared ocm client with custom
// settings.
func WithHTTPClient(client *http.Client) Option {
	return func(o *Options) {
		o.Client = client
	}
}

// WithMaxDownloadSize caps the number of bytes read from a tarball response.
// Zero or negative (the default) means unlimited: tarballs are streamed to disk
// rather than held in memory, so a download is bounded by free disk rather than
// by RAM.
func WithMaxDownloadSize(size int64) Option {
	return func(o *Options) {
		o.MaxDownloadSize = &size
	}
}
