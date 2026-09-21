package download

import (
	"hash"
	"net/http"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// DefaultMaxDownloadSize is the default cap: zero means unlimited. Bodies are
// streamed to disk, so a download is bounded by free disk, not RAM. Use
// [WithMaxDownloadSize] to cap.
const DefaultMaxDownloadSize int64 = 0

type option struct {
	Client           *http.Client
	MaxDownloadSize  *int64
	Credentials      runtime.Typed
	TempDir          string
	DigestAlgorithms []DigestAlgorithm
}

// Option configures [Download].
type Option func(*option)

// WithClient sets the HTTP client. Nil uses http.DefaultClient.
func WithClient(client *http.Client) Option {
	return func(o *option) {
		o.Client = client
	}
}

// WithMaxDownloadSize caps response body bytes. Zero or negative disables.
func WithMaxDownloadSize(size int64) Option {
	return func(o *option) {
		o.MaxDownloadSize = &size
	}
}

// WithTempDir sets the directory the response body is written to. Empty uses
// the OS temp directory.
func WithTempDir(dir string) Option {
	return func(o *option) {
		o.TempDir = dir
	}
}

// DigestAlgorithm pairs a caller-defined name with the hash used to compute
// it. The name is echoed back as the [Blob.Digests] key.
type DigestAlgorithm struct {
	Name string
	New  func() hash.Hash
}

// WithDigestAlgorithms computes hashes over the response body while it is
// streamed, exposing hex results on [Blob.Digests].
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
