package download

import (
	"net/http"

	"github.com/opencontainers/go-digest"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// DefaultMaxDownloadSize is the default maximum object size. Zero means unlimited:
// bodies are streamed through rather than held in memory or stored, so object size
// costs neither RAM nor disk. Use [WithMaxDownloadSize] to cap it.
const DefaultMaxDownloadSize int64 = 0

type option struct {
	HTTPClient      *http.Client
	Credentials     runtime.Typed
	MaxDownloadSize *int64
	HTTPConfig      *httpv1alpha1.Config
	DigestAlgorithm digest.Algorithm
}

// Option configures a download.
type Option func(*option)

func newOptions(opts []Option) *option {
	o := &option{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// maxDownloadSize returns the configured limit; zero or negative means unlimited.
func (o *option) maxDownloadSize() int64 {
	if o.MaxDownloadSize != nil {
		return *o.MaxDownloadSize
	}
	return DefaultMaxDownloadSize
}

// WithCredentials sets the OCM credentials used to build the S3 client. When nil,
// the AWS default credential chain is used. Anonymous access requires credentials
// with Anonymous set to true; credential or authorization errors never trigger it.
func WithCredentials(c runtime.Typed) Option {
	return func(o *option) { o.Credentials = c }
}

// WithMaxDownloadSize caps the number of bytes read from the object body.
// Zero (the default) means unlimited.
func WithMaxDownloadSize(size int64) Option {
	return func(o *option) { o.MaxDownloadSize = &size }
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

// WithDigestAlgorithm makes [Stream] compute the digest of the object with alg while
// streaming it, reported in [Result.Digest]. A [Blob] ignores it.
func WithDigestAlgorithm(alg digest.Algorithm) Option {
	return func(o *option) { o.DigestAlgorithm = alg }
}
