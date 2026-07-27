package download

import (
	"context"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// DefaultMaxDownloadSize is the default maximum object size. Zero means unlimited:
// bodies are streamed to disk rather than held in memory, so an object is bounded
// by free disk rather than by RAM. Use [WithMaxDownloadSize] to cap it.
const DefaultMaxDownloadSize int64 = 0

// ObjectGetter is the subset of the S3 client used by the downloader. The
// generated *s3.Client satisfies it, and tests can inject a fake.
type ObjectGetter interface {
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

type option struct {
	Client          ObjectGetter
	HTTPClient      *http.Client
	Credentials     runtime.Typed
	MaxDownloadSize *int64
	TempDir         string
	HTTPConfig      *httpv1alpha1.Config
}

// Option configures a download.
type Option func(*option)

// WithClient injects a pre-built S3 client (or a fake, in tests). When set, the
// downloader does not construct its own client from the request and credentials.
func WithClient(c ObjectGetter) Option {
	return func(o *option) { o.Client = c }
}

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

// WithHTTPConfig sets the HTTP configuration used to build the S3 client's
// underlying HTTP client. Ignored when a client is supplied via [WithHTTPClient].
// When nil, the shared client's defaults apply.
func WithHTTPConfig(cfg *httpv1alpha1.Config) Option {
	return func(o *option) { o.HTTPConfig = cfg }
}

// WithHTTPClient sets the HTTP client the S3 client sends its requests through.
// It is used exactly as given, so [WithHTTPConfig] and the request's
// InsecureSkipTLSVerify do not apply to it. When unset, a client is built from the
// HTTP config instead.
func WithHTTPClient(c *http.Client) Option {
	return func(o *option) { o.HTTPClient = c }
}
