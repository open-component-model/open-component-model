package download

import (
	"net/http"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// DefaultMaxDownloadSize is the default maximum download size. Zero means unlimited:
// bodies are streamed to disk rather than held in memory, so a download is bounded
// by free disk rather than by RAM. Use [WithMaxDownloadSize] to cap it.
const DefaultMaxDownloadSize int64 = 0

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

	// TempDir is the directory the response body is written to. Empty uses the OS
	// temporary directory.
	TempDir string
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

// WithMaxDownloadSize caps the number of bytes read from a response body.
// Zero or negative (the default) means unlimited.
func WithMaxDownloadSize(size int64) Option {
	return func(o *option) {
		o.MaxDownloadSize = &size
	}
}

// WithTempDir sets the directory the response body is written to. Empty uses the
// OS temporary directory. The file backing the returned blob is created here and
// outlives [Download], so the caller owns its lifetime.
func WithTempDir(dir string) Option {
	return func(o *option) {
		o.TempDir = dir
	}
}

// WithCredentials sets the OCM credentials applied to the request.
func WithCredentials(credentials runtime.Typed) Option {
	return func(o *option) {
		o.Credentials = credentials
	}
}
