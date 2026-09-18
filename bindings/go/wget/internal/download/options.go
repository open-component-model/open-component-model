package download

import (
	"hash"
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

	// DigestAlgorithms names the hashes computed over the body during download, in
	// addition to the media type handling. Each name is a crypto.Hash algorithm
	// (identified by the caller); the computed hex digests are exposed on the
	// returned Blob via [Blob.Digests]. When empty, no digest is computed during
	// the stream (the Blob still computes SHA-256 lazily on demand).
	DigestAlgorithms []DigestAlgorithm
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

// DigestAlgorithm pairs a caller-defined name with the hash used to compute it.
// The name is echoed back as the key in [Blob.Digests], letting the caller map a
// computed hex digest back to its own algorithm identifier.
type DigestAlgorithm struct {
	// Name is the key the computed hex digest is exposed under in [Blob.Digests].
	Name string
	// New constructs a fresh hash for the algorithm.
	New func() hash.Hash
}

// WithDigestAlgorithms computes the given hashes over the response body while it
// is streamed to disk, exposing the hex results on the returned [Blob.Digests].
func WithDigestAlgorithms(algs ...DigestAlgorithm) Option {
	return func(o *option) {
		o.DigestAlgorithms = algs
	}
}

// WithCredentials sets the OCM credentials applied to the request.
func WithCredentials(credentials runtime.Typed) Option {
	return func(o *option) {
		o.Credentials = credentials
	}
}
