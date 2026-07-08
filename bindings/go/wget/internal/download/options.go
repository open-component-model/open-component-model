package download

import (
	"net/http"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// DefaultMaxDownloadSize is the maximum download size (100 MiB) applied when a
// [Download] call does not set one via [WithMaxDownloadSize].
const DefaultMaxDownloadSize int64 = 100 * 1024 * 1024

// option holds the configuration for a single [Download] call.
type option struct {
	// Client is the HTTP client used for the request. When nil, http.DefaultClient is used.
	Client *http.Client

	// MaxDownloadSize limits the number of bytes read from a response body. When nil,
	// DefaultMaxDownloadSize is applied; a zero or negative value disables the limit.
	MaxDownloadSize *int64

	// Credentials are the OCM credentials applied to the request. When nil, the
	// request is sent unauthenticated.
	Credentials runtime.Typed
}

// Option configures the behavior of [Download].
type Option func(*option)

// WithClient sets the HTTP client used for the download. When unset,
// http.DefaultClient is used.
func WithClient(client *http.Client) Option {
	return func(o *option) {
		o.Client = client
	}
}

// WithMaxDownloadSize limits the number of bytes read from a response body.
// A zero or negative value disables the limit (not recommended for untrusted sources).
// When this option is not supplied, [DefaultMaxDownloadSize] is applied.
func WithMaxDownloadSize(size int64) Option {
	return func(o *option) {
		o.MaxDownloadSize = &size
	}
}

// WithCredentials sets the OCM credentials applied to the request.
func WithCredentials(credentials runtime.Typed) Option {
	return func(o *option) {
		o.Credentials = credentials
	}
}
