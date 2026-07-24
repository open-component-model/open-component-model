package download

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// DefaultMaxDownloadSize is the default maximum object size read into memory.
// Zero means unlimited; callers can cap it with [WithMaxDownloadSize].
const DefaultMaxDownloadSize int64 = 0

// ObjectGetter is the subset of the S3 client used by the downloader. The
// generated *s3.Client satisfies it, and tests can inject a fake.
type ObjectGetter interface {
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

type option struct {
	Client          ObjectGetter
	Credentials     runtime.Typed
	MaxDownloadSize *int64
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
