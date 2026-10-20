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
// have side effects. A reader closed before it consumed any content hands its
// response back, so the next reader takes it without sending the request again.
//
// The request carries the context passed to [Download], so the blob can only be
// read while that context is alive.
//
// While no reader holds a response, the blob holds it. [Blob.Close] releases
// it, and a cleanup attached to the blob releases it once the blob becomes
// unreachable, so a caller dropping an unread blob does not leak it.
type Blob struct {
	fetch      *fetcher
	algorithms []DigestAlgorithm
	verify     Verifier
	// headers are the response headers of the first response.
	headers http.Header
	// size is the content size known from the first response, or [blob.SizeUnknown].
	// Every response is checked against it.
	size      int64
	mediaType atomic.Pointer[string]
	// precalculated is returned by [Blob.Digest] verbatim when set.
	precalculated atomic.Pointer[string]
	// pending is the response the next reader takes, if any.
	pending atomic.Pointer[pendingResponse]
	closed  atomic.Bool
}

// pendingResponse is a response held by the blob, together with the cleanup
// that closes it if the blob becomes unreachable.
type pendingResponse struct {
	rep     representation
	cleanup runtime.Cleanup
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
	b.hold(rep)
	return b
}

// hold makes rep the response the next reader takes. It reports false, leaving
// rep to the caller, if the blob already holds a response.
func (b *Blob) hold(rep representation) bool {
	p := &pendingResponse{rep: rep}
	// The cleanup takes the body rather than the blob: an argument that can reach
	// the object the cleanup is attached to would keep it alive forever.
	p.cleanup = runtime.AddCleanup(b, closeBody, io.Closer(rep.resp.Body))
	if !b.pending.CompareAndSwap(nil, p) {
		p.cleanup.Stop()
		return false
	}
	// A concurrent Close may have looked for a held response before it was stored.
	if b.closed.Load() {
		if rep, ok := b.take(); ok {
			_ = rep.resp.Body.Close()
		}
	}
	return true
}

// take removes the held response, if any, and returns it to the caller.
func (b *Blob) take() (representation, bool) {
	p := b.pending.Swap(nil)
	if p == nil {
		return representation{}, false
	}
	p.cleanup.Stop()
	return p.rep, true
}

func closeBody(body io.Closer) {
	_ = body.Close()
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

// Close releases the held response, if any. It is safe to call multiple times
// and from multiple goroutines. Readers already returned stay usable; new ones
// are refused.
func (b *Blob) Close() error {
	b.closed.Store(true)
	if rep, ok := b.take(); ok {
		return rep.resp.Body.Close()
	}
	return nil
}

// ReadCloser returns a reader over the response body. It takes the held
// response, if any, and otherwise sends the request again. Size limit,
// announced size and [Verifier] are checked when the reader reaches the end of
// the body; a failure is returned from that Read and from Close.
func (b *Blob) ReadCloser() (io.ReadCloser, error) {
	if b.closed.Load() {
		return nil, fmt.Errorf("download of %s is closed", b.fetch.safeURL)
	}
	rep, ok := b.take()
	if !ok {
		var err error
		if rep, err = b.reread(); err != nil {
			return nil, err
		}
	}

	return &bodyReader{
		blob:     b,
		rep:      rep,
		expected: b.size,
		hashers:  newHashers(b.algorithms),
	}, nil
}

// reread sends the request again. A response announcing a size other than the
// one known from the first response is rejected here; one without a length is
// checked against it while it is read.
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
	if b.size >= 0 && rep.size >= 0 && rep.size != b.size {
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
	blob *Blob
	// rep is the response read; its size is the length the response announced,
	// or [blob.SizeUnknown].
	rep representation
	// expected is the content size known from the first response, or
	// [blob.SizeUnknown].
	expected int64
	hashers  map[string]hash.Hash
	read     int64
	// err is the terminal error of the reader; once set, it is returned by every
	// Read and by Close.
	err    error
	done   bool
	closed bool
}

func (r *bodyReader) Read(p []byte) (int, error) {
	if r.closed {
		return 0, fmt.Errorf("read of response body from %s after close", r.blob.fetch.safeURL)
	}
	if r.err != nil {
		return 0, r.err
	}
	if r.done {
		return 0, io.EOF
	}

	n, err := r.rep.resp.Body.Read(p)
	r.read += int64(n)
	if maxSize := r.blob.fetch.maxSize; maxSize > 0 && r.read > maxSize {
		r.err = r.blob.fetch.tooLarge()
		return 0, r.err
	}
	if r.expected >= 0 && r.read > r.expected {
		r.err = r.sizeMismatch()
		return 0, r.err
	}
	for _, h := range r.hashers {
		h.Write(p[:n])
	}

	// Consumers that know the size may stop reading there without seeing EOF,
	// so the end of a body of announced length is its last byte.
	if errors.Is(err, io.EOF) || (r.rep.size >= 0 && r.read == r.rep.size) {
		if r.err = r.finish(); r.err != nil {
			return n, r.err
		}
		r.done = true
		return n, io.EOF
	}
	if err != nil {
		r.err = err
	}
	return n, err
}

func (r *bodyReader) finish() error {
	if r.expected >= 0 && r.read != r.expected {
		return r.sizeMismatch()
	}
	if r.blob.verify == nil {
		return nil
	}
	computed := Computed{Content: sums(r.hashers)}
	computed.Pinned, _ = r.blob.Digest()
	return r.blob.verify(r.rep.resp.Header, computed)
}

func (r *bodyReader) sizeMismatch() error {
	return fmt.Errorf("response body from %s does not have the expected %d bytes", r.blob.fetch.safeURL, r.expected)
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
//
// A consumer that knows the size may stop there without reading at all (empty
// content) or without seeing the end of a response that announced no length, so
// Close reads on to the end of the body and checks it. A reader that consumed
// nothing hands its response back to the blob instead of closing it.
func (r *bodyReader) Close() error {
	if r.closed {
		return r.err
	}
	if !r.done && r.err == nil && r.expected >= 0 && r.read == r.expected {
		_, _ = r.Read(make([]byte, 1))
	}
	r.closed = true
	if r.read == 0 && r.err == nil && r.blob.hold(r.rep) {
		return nil
	}
	return errors.Join(r.err, r.rep.resp.Body.Close())
}
