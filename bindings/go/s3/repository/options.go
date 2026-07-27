package repository

import (
	"net/http"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/s3/internal/download"
)

// Options holds configuration for the S3 resource repository.
type Options struct {
	// client injects a pre-built S3 client in tests. It is deliberately not
	// settable from outside this package: a client bakes in one endpoint, region
	// and identity, while a repository serves resources whose access specs each
	// carry their own. Injecting one would silently route every download to the
	// same place and ignore the credentials handed to DownloadResource. When nil
	// (always, outside tests) a client is built per download from the access spec
	// and credentials, which is what makes multi-bucket use correct.
	client download.ObjectGetter
	// MaxDownloadSize caps the number of bytes read from an object. Nil uses the
	// default (unlimited).
	MaxDownloadSize *int64
	// HTTPConfig configures the HTTP client used to reach S3. Ignored when
	// HTTPClient is set. Nil uses the shared client's defaults.
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
// wire and reconstruct an equivalent client. Ignored when [WithHTTPClient] is used.
func WithHTTPConfig(cfg *httpv1alpha1.Config) Option {
	return func(o *Options) {
		o.HTTPConfig = cfg
	}
}

// WithHTTPClient sets the HTTP client used for object downloads. Unlike an S3
// client, an HTTP client carries no bucket, region or endpoint of its own — those
// come from each resource's access spec — so one client serves them all correctly.
//
// It is used exactly as given: its TLS, timeout and retry behaviour belong to the
// caller, so neither [WithHTTPConfig] nor the access spec's insecureSkipTLSVerify
// applies to it. Callers wanting the shared ocm client with their own settings can
// build one with ocmhttp.New and pass it here.
func WithHTTPClient(client *http.Client) Option {
	return func(o *Options) {
		o.HTTPClient = client
	}
}
