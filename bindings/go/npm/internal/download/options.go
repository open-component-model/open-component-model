package download

import (
	"net/http"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// DefaultMaxDownloadSize is the default maximum tarball size. Zero means unlimited:
// tarballs are streamed to disk rather than held in memory, so a download is bounded
// by free disk rather than by RAM. Use [WithMaxDownloadSize] to cap it.
const DefaultMaxDownloadSize int64 = 0

// DefaultMaxMetadataSize caps the registry metadata document. The version document
// of a package is a few kilobytes, but the packument fallback carries every
// version of a package and decodes into a map held in memory.
const DefaultMaxMetadataSize int64 = 64 << 20

// option holds the configuration for a single [Download] call.
type option struct {
	// Client is the HTTP client used for the requests. When nil, http.DefaultClient is used.
	Client *http.Client

	// MaxDownloadSize limits the number of bytes read from the tarball response. When nil,
	// DefaultMaxDownloadSize is applied; a zero or negative value disables the limit.
	MaxDownloadSize *int64

	// MaxMetadataSize limits the number of bytes read from a metadata response. When nil,
	// DefaultMaxMetadataSize is applied; a zero or negative value disables the limit.
	MaxMetadataSize *int64

	// Credentials are the OCM credentials applied to the registry requests. When nil,
	// the requests are sent unauthenticated.
	Credentials runtime.Typed

	// TempDir is the directory the tarball is written to. Empty uses the OS
	// temporary directory.
	TempDir string
}

func (o *option) maxDownloadSize() int64 {
	if o.MaxDownloadSize == nil {
		return DefaultMaxDownloadSize
	}
	return *o.MaxDownloadSize
}

func (o *option) maxMetadataSize() int64 {
	if o.MaxMetadataSize == nil {
		return DefaultMaxMetadataSize
	}
	return *o.MaxMetadataSize
}

func (o *option) client() *http.Client {
	if o.Client == nil {
		return http.DefaultClient
	}
	return o.Client
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

// WithMaxDownloadSize caps the number of bytes read from the tarball response.
// Zero or negative (the default) means unlimited.
func WithMaxDownloadSize(size int64) Option {
	return func(o *option) {
		o.MaxDownloadSize = &size
	}
}

// WithMaxMetadataSize caps the number of bytes read from a registry metadata
// response. Zero or negative means unlimited.
func WithMaxMetadataSize(size int64) Option {
	return func(o *option) {
		o.MaxMetadataSize = &size
	}
}

// WithTempDir sets the directory the tarball is written to. Empty uses the OS
// temporary directory. The file backing the returned blob is created here and
// outlives [Download], so the caller owns its lifetime.
func WithTempDir(dir string) Option {
	return func(o *option) {
		o.TempDir = dir
	}
}

// WithCredentials sets the OCM credentials applied to the registry requests.
func WithCredentials(credentials runtime.Typed) Option {
	return func(o *option) {
		o.Credentials = credentials
	}
}
