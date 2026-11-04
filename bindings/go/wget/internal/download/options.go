package download

import (
	"hash"
	"net/http"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// DefaultMaxDownloadSize is the default maximum download size. Zero means unlimited:
// bodies are streamed to the reader rather than held in memory or on disk, so a
// download is bounded only by what the consumer accepts. Use [WithMaxDownloadSize]
// to cap it.
const DefaultMaxDownloadSize int64 = 0

// option holds the configuration for a single [Download] call.
type option struct {
	// Client is the HTTP client used for the request. When nil, a client from
	// ocmhttp.New is used, which returns response bytes unmodified.
	Client *http.Client

	// MaxDownloadSize limits the number of bytes read from a response body. When nil,
	// DefaultMaxDownloadSize is applied; a zero or negative value disables the limit.
	MaxDownloadSize *int64

	// Credentials are the OCM credentials applied to the request. When nil, the
	// request is sent unauthenticated.
	Credentials runtime.Typed

	// DigestAlgorithms are hashes computed over the response body by every reader.
	DigestAlgorithms []DigestAlgorithm

	// Verifier checks every fully read response body.
	Verifier Verifier

	// HeaderCheck checks the header of every response before its body is read.
	HeaderCheck HeaderCheck

	// AdvertisedDigest resolves the digest the blob reports from the headers of
	// the learned metadata.
	AdvertisedDigest AdvertisedDigest
}

// Option configures the behavior of [Download].
type Option func(*option)

// WithClient sets the HTTP client used for the download. When unset, a client
// from ocmhttp.New is used, which returns response bytes unmodified.
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

// DigestAlgorithm pairs a caller-defined name with the hash used to compute
// it. The name is the key of the computed digests handed to the [Verifier].
type DigestAlgorithm struct {
	Name string
	New  func() hash.Hash
}

// WithDigestAlgorithms computes hashes over the response body while a reader
// streams it, handing the hex results to the [Verifier] at the end of the body.
func WithDigestAlgorithms(algs ...DigestAlgorithm) Option {
	return func(o *option) {
		o.DigestAlgorithms = algs
	}
}

// WithVerifier runs v when a reader of the blob reaches the end of the body.
// An error from v fails that reader's final Read and its Close.
func WithVerifier(v Verifier) Option {
	return func(o *option) {
		o.Verifier = v
	}
}

// HeaderCheck checks a response header before the body is read. An error
// fails the reader's first Read without reading the body.
type HeaderCheck func(header http.Header) error

// WithHeaderCheck runs check on the header of every response before its body
// is handed to a reader.
func WithHeaderCheck(check HeaderCheck) Option {
	return func(o *option) {
		o.HeaderCheck = check
	}
}

// AdvertisedDigest returns the digest a response header advertises for the
// content, in the form reported by [Blob.Digest], or false if it advertises none.
type AdvertisedDigest func(header http.Header) (string, bool)

// WithAdvertisedDigest makes [Blob.Digest] report the digest advertised by the
// headers the blob's metadata is learned from, unless a precalculated digest is
// set. Like a precalculated digest, it is handed to the [Verifier] as
// [Computed.Pinned].
func WithAdvertisedDigest(advertised AdvertisedDigest) Option {
	return func(o *option) {
		o.AdvertisedDigest = advertised
	}
}

// WithCredentials sets the OCM credentials applied to the request.
func WithCredentials(credentials runtime.Typed) Option {
	return func(o *option) {
		o.Credentials = credentials
	}
}
