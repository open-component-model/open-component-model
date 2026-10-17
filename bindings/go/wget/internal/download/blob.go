package download

import (
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"runtime"
	"sync/atomic"

	"ocm.software/open-component-model/bindings/go/blob"
)

// Blob is the blob returned by [Download]. It streams the HTTP response instead of
// buffering it: the first [Blob.ReadCloser] reads the response [Download] opened,
// and every later one sends the request again, so the blob stays re-readable.
// Only GET and HEAD downloads are re-readable: sending other methods again may
// have side effects.
//
// The request carries the context passed to [Download], so the blob can only be
// read while that context is alive.
//
// Until the first response is read, the blob holds its connection. [Blob.Close]
// releases it, and a cleanup attached to the blob releases it once the blob
// becomes unreachable, so a caller dropping an unread blob does not leak it.
type Blob struct {
	fetch      *fetcher
	algorithms []DigestAlgorithm
	verify     Verifier
	// headers are the response headers of the first response.
	headers http.Header
	// size is the content size known from the first response, or [blob.SizeUnknown].
	size      int64
	mediaType atomic.Pointer[string]
	// precalculated is returned by [Blob.Digest] verbatim when set.
	precalculated atomic.Pointer[string]
	// pending is the first response until a reader or Close takes it.
	pending atomic.Pointer[http.Response]
	cleanup runtime.Cleanup
	closed  atomic.Bool
}

var (
	_ blob.ReadOnlyBlob          = (*Blob)(nil)
	_ blob.SizeAware             = (*Blob)(nil)
	_ blob.DigestAware           = (*Blob)(nil)
	_ blob.DigestPrecalculatable = (*Blob)(nil)
	_ blob.MediaTypeAware        = (*Blob)(nil)
	_ blob.MediaTypeOverrideable = (*Blob)(nil)
	_ io.Closer                  = (*Blob)(nil)
)

func newBlob(f *fetcher, rep representation, mediaType string, o *option) *Blob {
	b := &Blob{
		fetch:      f,
		algorithms: o.DigestAlgorithms,
		verify:     o.Verifier,
		headers:    rep.resp.Header,
		size:       rep.size,
	}
	b.SetMediaType(mediaType)
	b.pending.Store(rep.resp)
	// The cleanup takes the body rather than the blob: an argument that can reach
	// the object the cleanup is attached to would keep it alive forever.
	b.cleanup = runtime.AddCleanup(b, func(body io.Closer) { _ = body.Close() }, io.Closer(rep.resp.Body))
	return b
}

// Headers returns the response headers of the first response.
func (b *Blob) Headers() http.Header {
	return b.headers
}

// Size returns the content size announced by the first response, or
// [blob.SizeUnknown].
func (b *Blob) Size() int64 {
	return b.size
}

// MediaType returns the media type of the blob.
func (b *Blob) MediaType() (string, bool) {
	return *b.mediaType.Load(), true
}

// SetMediaType overrides the media type of the blob.
func (b *Blob) SetMediaType(mediaType string) {
	b.mediaType.Store(&mediaType)
}

// Digest returns the precalculated digest when set. The content is never read to
// compute a digest, so without one it reports false.
func (b *Blob) Digest() (string, bool) {
	if p := b.precalculated.Load(); p != nil {
		return *p, true
	}
	return "", false
}

// HasPrecalculatedDigest reports whether a precalculated digest was set.
func (b *Blob) HasPrecalculatedDigest() bool {
	return b.precalculated.Load() != nil
}

// SetPrecalculatedDigest sets the digest returned by [Blob.Digest]. Safe for
// concurrent use. It is handed to the [Verifier] as [Computed.Pinned], which has
// to check it against the content.
func (b *Blob) SetPrecalculatedDigest(digest string) {
	b.precalculated.Store(&digest)
}

// Close releases the first response if no reader took it. It is safe to call
// multiple times and from multiple goroutines. Readers already returned stay
// usable; new ones are refused.
func (b *Blob) Close() error {
	b.closed.Store(true)
	if resp := b.pending.Swap(nil); resp != nil {
		b.cleanup.Stop()
		return resp.Body.Close()
	}
	return nil
}

// ReadCloser returns a reader over the response body. The first call reads the
// response opened by [Download]; later calls send the request again. Size limit,
// announced size and [Verifier] are checked when the reader reaches the end of
// the body; a failure is returned from that Read and from Close.
func (b *Blob) ReadCloser() (io.ReadCloser, error) {
	if b.closed.Load() {
		return nil, fmt.Errorf("download of %s is closed", b.fetch.safeURL)
	}
	rep := representation{resp: b.pending.Swap(nil), size: b.size}
	if rep.resp != nil {
		b.cleanup.Stop()
	} else {
		var err error
		if rep, err = b.reread(); err != nil {
			return nil, err
		}
	}

	return &bodyReader{
		blob:    b,
		body:    rep.resp.Body,
		header:  rep.resp.Header,
		size:    rep.size,
		hashers: newHashers(b.algorithms),
	}, nil
}

// reread sends the request again. The size known from the first response stays
// authoritative.
func (b *Blob) reread() (representation, error) {
	if m := b.fetch.request.Method; m != http.MethodGet && m != http.MethodHead {
		return representation{}, fmt.Errorf("download of %s cannot be read again: sending the %s request again may have side effects", b.fetch.safeURL, m)
	}
	r, err := b.fetch.clone()
	if err != nil {
		return representation{}, err
	}
	rep, err := b.fetch.send(r)
	if err != nil {
		return representation{}, err
	}
	switch {
	case rep.size < 0:
		rep.size = b.size
	case b.size >= 0 && rep.size != b.size:
		_ = rep.resp.Body.Close()
		return representation{}, fmt.Errorf("response body from %s changed size between reads: expected %d bytes, got %d", b.fetch.safeURL, b.size, rep.size)
	}
	return rep, nil
}

func newHashers(algorithms []DigestAlgorithm) map[string]hash.Hash {
	hashers := make(map[string]hash.Hash, len(algorithms))
	for _, alg := range algorithms {
		hashers[alg.Name] = alg.New()
	}
	return hashers
}

// bodyReader streams one response body, hashing it and checking it at its end.
type bodyReader struct {
	blob   *Blob
	body   io.ReadCloser
	header http.Header
	// size is the content size, or [blob.SizeUnknown].
	size    int64
	hashers map[string]hash.Hash
	read    int64
	// err is the terminal error of the reader; once set, it is returned by every
	// Read and by Close.
	err  error
	done bool
}

func (r *bodyReader) Read(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	if r.done {
		return 0, io.EOF
	}

	n, err := r.body.Read(p)
	r.read += int64(n)
	if maxSize := r.blob.fetch.maxSize; maxSize > 0 && r.read > maxSize {
		r.err = r.blob.fetch.tooLarge()
		return 0, r.err
	}
	for _, h := range r.hashers {
		h.Write(p[:n])
	}

	// Consumers that know the size may stop reading there without seeing EOF,
	// so the end of a body of known size is its last byte.
	if errors.Is(err, io.EOF) || (r.size >= 0 && r.read == r.size) {
		if r.err = r.finish(); r.err != nil {
			return n, r.err
		}
		r.done = true
		return n, io.EOF
	}
	return n, err
}

func (r *bodyReader) finish() error {
	if r.size >= 0 && r.read != r.size {
		return fmt.Errorf("response body from %s has %d bytes, expected %d", r.blob.fetch.safeURL, r.read, r.size)
	}
	if r.blob.verify == nil {
		return nil
	}
	computed := Computed{Content: sums(r.hashers)}
	computed.Pinned, _ = r.blob.Digest()
	return r.blob.verify(r.header, computed)
}

func sums(hashers map[string]hash.Hash) map[string]string {
	out := make(map[string]string, len(hashers))
	for name, h := range hashers {
		out[name] = hex.EncodeToString(h.Sum(nil))
	}
	return out
}

// Close closes the response body. It also returns the terminal error of the
// reader, so callers that drop the error of the last Read still see it.
func (r *bodyReader) Close() error {
	return errors.Join(r.err, r.body.Close())
}
