package download

import (
	"net/http"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// DefaultMaxDownloadSize is the default maximum object size. Zero means unlimited:
// bodies are streamed to disk rather than held in memory, so an object is bounded
// by free disk rather than by RAM. Use [WithMaxDownloadSize] to cap it.
const DefaultMaxDownloadSize int64 = 0

type option struct {
	HTTPClient      *http.Client
	Credentials     runtime.Typed
	MaxDownloadSize *int64
	TempDir         string
	HTTPConfig      *httpv1alpha1.Config
}

// Option configures a download.
type Option func(*option)

// WithCredentials sets the OCM credentials used to build the S3 client. When nil,
// the AWS default credential chain is used.
func WithCredentials(c runtime.Typed) Option {
	return func(o *option) { o.Credentials = c }
}

// WithMaxDownloadSize caps the number of bytes read from the object body.
// Zero (the default) means unlimited.
func WithMaxDownloadSize(size int64) Option {
	return func(o *option) { o.MaxDownloadSize = &size }
}

// WithTempDir sets the directory the downloaded object is written to. Empty uses
// the OS temporary directory. The file backing the returned blob is created here
// and outlives [Download], so the caller owns its lifetime.
func WithTempDir(dir string) Option {
	return func(o *option) { o.TempDir = dir }
}

// WithHTTPConfig sets the HTTP configuration used to build the S3 client's underlying
// HTTP client. When nil, the shared client's defaults apply.
func WithHTTPConfig(cfg *httpv1alpha1.Config) Option {
	return func(o *option) { o.HTTPConfig = cfg }
}

// WithHTTPClient sets the HTTP client the S3 client sends its requests through.
// It is used exactly as given, so the transport settings of [WithHTTPConfig] do not
// apply to it. When unset, a client is built from the HTTP config instead.
func WithHTTPClient(c *http.Client) Option {
	return func(o *option) { o.HTTPClient = c }
}
