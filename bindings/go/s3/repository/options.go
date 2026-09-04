package repository

import (
	"net/http"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
)

// Options holds configuration for the S3 resource repository.
type Options struct {
	// MaxDownloadSize caps the number of bytes read from an object. Nil uses the
	// default (unlimited).
	MaxDownloadSize *int64
	// HTTPConfig configures the HTTP client used to reach S3, and its retry section
	// additionally drives the SDK's attempt count even when HTTPClient is set. Nil
	// uses the shared client's defaults.
	HTTPConfig *httpv1alpha1.Config
	// HTTPClient is the HTTP client object downloads are sent through. Nil builds
	// one per download from HTTPConfig.
	HTTPClient *http.Client
}

// Option configures Options.
type Option func(*Options)

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
// wire and reconstruct an equivalent client. Its transport settings are ignored when
// [WithHTTPClient] is used; its retry section drives the SDK either way.
func WithHTTPConfig(cfg *httpv1alpha1.Config) Option {
	return func(o *Options) {
		o.HTTPConfig = cfg
	}
}

// WithHTTPClient sets the HTTP client used for object downloads. Unlike an S3
// client, it carries no bucket, region or endpoint of its own, so one client serves
// every resource correctly.
//
// It is used exactly as given, so the transport settings of [WithHTTPConfig] do not
// apply to it. Build one with ocmhttp.New to get the shared ocm client with custom
// settings.
func WithHTTPClient(client *http.Client) Option {
	return func(o *Options) {
		o.HTTPClient = client
	}
}
