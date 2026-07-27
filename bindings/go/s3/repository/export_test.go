package repository

import (
	"ocm.software/open-component-model/bindings/go/s3/internal/download"
)

// WithClient injects a pre-built S3 client so tests can serve canned objects
// without a bucket. It lives here rather than in options.go because it is only
// sound when every resource shares one endpoint, region and identity, which is
// true of a test and not of a real repository. This file is compiled only when
// testing this package, so the option is not part of the public API.
func WithClient(client download.ObjectGetter) Option {
	return func(o *Options) {
		o.client = client
	}
}
