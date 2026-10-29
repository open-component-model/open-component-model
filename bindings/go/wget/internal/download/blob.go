package download

import (
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"net/http"
	"sync"

	"ocm.software/open-component-model/bindings/go/blob"
)

// Blob is the blob returned by [Download]. It streams the HTTP response instead of
// buffering it and sends no request until it is used. Every reader sends the
// request on its first Read and releases the response on Close; a reader closed
// before its first Read sends nothing. Only GET and HEAD downloads are sent more
// than once: sending other methods again may have side effects, so their blob
// can be read once.
//
// Size, media type and the advertised digest (see [WithAdvertisedDigest]) are
// learned once: from a HEAD request sent the first time one of them is asked
// for, or from the first reader's response if that comes first. Other methods
// than GET and HEAD send no HEAD request, so their metadata is unknown until a
// reader got its response. Every reader is checked against the learned
// metadata: a response announcing another size is rejected, and a reported
// digest is handed to the [Verifier] as [Computed.Pinned].
//
// The requests carry the context passed to [Download], so the blob can only be
// read while that context is alive.
type Blob struct {
	fetch      *fetcher
	algorithms []DigestAlgorithm
	verify     Verifier
	check      HeaderCheck
	advertised AdvertisedDigest

	// probe serializes metadata requests, so at most one HEAD request is sent.
	probe sync.Mutex

	mu sync.Mutex
	// mediaType is the media type set by the request or [Blob.SetMediaType];
	// empty means the media type of the learned metadata.
	mediaType string
	// precalculated is returned by [Blob.Digest] verbatim when set.
	precalculated *string
	// meta is the learned metadata, or nil until it is learned.
	meta *metadata
	// sent reports whether a reader sent the request.
	sent bool
}

// metadata describes the content as announced by the first response seen.
type metadata struct {
	// size is the content size, or [blob.SizeUnknown].
	size        int64
	contentType string
	// digest is the advertised digest, or empty.
	digest string
}

// representation is a response together with how its body maps to the content.
type representation struct {
	resp *http.Response
	// size is the content size, or [blob.SizeUnknown].
	size int64
}

// bodySize returns the number of bytes resp's body yields, or [blob.SizeUnknown].
func bodySize(resp *http.Response) int64 {
	switch {
	case resp.Request != nil && resp.Request.Method == http.MethodHead:
		return 0
	case resp.ContentLength >= 0 && !resp.Uncompressed:
		return resp.ContentLength
	default:
		return blob.SizeUnknown
	}
}

var (
	_ blob.ReadOnlyBlob          = (*Blob)(nil)
	_ blob.SizeAware             = (*Blob)(nil)
	_ blob.DigestAware           = (*Blob)(nil)
	_ blob.DigestPrecalculatable = (*Blob)(nil)
	_ blob.MediaTypeAware        = (*Blob)(nil)
	_ blob.MediaTypeOverrideable = (*Blob)(nil)
)

func newBlob(f *fetcher, mediaType string, o *option) *Blob {
	return &Blob{
		fetch:      f,
		algorithms: o.DigestAlgorithms,
		verify:     o.Verifier,
		check:      o.HeaderCheck,
		advertised: o.AdvertisedDigest,
		mediaType:  mediaType,
	}
}

// metadata returns the learned metadata, sending a HEAD request to learn it if
// no response was seen yet.
func (b *Blob) metadata() metadata {
	if m, ok := b.learned(); ok {
		return m
	}
	b.probe.Lock()
	defer b.probe.Unlock()
	if m, ok := b.learned(); ok {
		return m
	}
	header, size := b.head()
	return b.learn(header, size)
}

func (b *Blob) learned() (metadata, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.meta == nil {
		return metadata{}, false
	}
	return *b.meta, true
}

// learn records the metadata announced by header unless metadata was already
// learned, and returns the learned metadata. A nil header announces nothing.
func (b *Blob) learn(header http.Header, size int64) metadata {
	m := metadata{size: size}
	if header != nil {
		m.contentType = header.Get("Content-Type")
		if b.advertised != nil {
			m.digest, _ = b.advertised(header)
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.meta == nil {
		b.meta = &m
	}
	return *b.meta
}

// head sends the request as a HEAD request and returns the response header and
// the announced content size. It returns a nil header for other methods than
// GET and HEAD and when the request fails, so the metadata stays unknown.
func (b *Blob) head() (http.Header, int64) {
	if !b.fetch.repeatable() {
		return nil, blob.SizeUnknown
	}
	ctx := b.fetch.request.Context()
	r, err := b.fetch.clone()
	if err != nil {
		slog.DebugContext(ctx, "http: cannot build HEAD request for metadata", "url", b.fetch.safeURL, "err", err)
		return nil, blob.SizeUnknown
	}
	isHead := r.Method == http.MethodHead
	r.Method = http.MethodHead
	resp, err := b.fetch.client.Do(r)
	if err != nil {
		slog.DebugContext(ctx, "http: HEAD request for metadata failed", "url", b.fetch.safeURL, "err", err)
		return nil, blob.SizeUnknown
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.DebugContext(ctx, "http: HEAD request for metadata failed", "url", b.fetch.safeURL, "status", resp.StatusCode)
		return nil, blob.SizeUnknown
	}
	// The HEAD response of a HEAD download describes its empty body. Otherwise
	// Content-Length is the length a GET would yield: the request asks for an
	// explicit encoding, so the transport never decodes the body.
	if isHead {
		return resp.Header, 0
	}
	return resp.Header, resp.ContentLength
}

// Size returns the content size, or [blob.SizeUnknown].
func (b *Blob) Size() int64 {
	return b.metadata().size
}

// MediaType returns the media type of the blob: the one set by the request or
// [Blob.SetMediaType], otherwise the announced Content-Type, falling back to
// application/octet-stream.
func (b *Blob) MediaType() (string, bool) {
	b.mu.Lock()
	mediaType := b.mediaType
	b.mu.Unlock()
	if mediaType != "" {
		return mediaType, true
	}
	if contentType := b.metadata().contentType; contentType != "" {
		return contentType, true
	}
	return "application/octet-stream", true
}

// SetMediaType overrides the media type of the blob.
func (b *Blob) SetMediaType(mediaType string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.mediaType = mediaType
}

// Digest returns the precalculated digest when set, otherwise the advertised
// digest. The content is never read to compute a digest, so without either it
// reports false.
func (b *Blob) Digest() (string, bool) {
	if d, ok := b.precalculatedDigest(); ok {
		return d, true
	}
	// Without an advertised-digest resolver a HEAD request cannot yield a digest.
	if b.advertised == nil {
		return "", false
	}
	if d := b.metadata().digest; d != "" {
		return d, true
	}
	return "", false
}

func (b *Blob) precalculatedDigest() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.precalculated == nil {
		return "", false
	}
	return *b.precalculated, true
}

// pinned returns the digest the blob reports without sending a request. Readers
// call it after their response was learned, so it matches [Blob.Digest].
func (b *Blob) pinned() string {
	if d, ok := b.precalculatedDigest(); ok {
		return d
	}
	m, _ := b.learned()
	return m.digest
}

// HasPrecalculatedDigest reports whether [Blob.Digest] knows a digest.
func (b *Blob) HasPrecalculatedDigest() bool {
	_, ok := b.Digest()
	return ok
}

// SetPrecalculatedDigest sets the digest returned by [Blob.Digest]. Safe for
// concurrent use. It is handed to the [Verifier] as [Computed.Pinned], which has
// to check it against the content.
func (b *Blob) SetPrecalculatedDigest(digest string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.precalculated = &digest
}

// ReadCloser returns a reader over the content. It sends the request on its
// first Read; closing it releases the response. Size limit, announced size and
// [Verifier] are checked when the reader reaches the end of the body; a failure
// is returned from that Read and from Close.
func (b *Blob) ReadCloser() (io.ReadCloser, error) {
	// Content known to be empty is consumed without a single Read, e.g. by a
	// consumer copying Size bytes. Its response is opened right away, so that
	// closing the reader still checks it.
	if m, ok := b.learned(); ok && m.size == 0 {
		return b.open()
	}
	return blob.ToReadCloser((*opener)(b)), nil
}

// opener is the blob as seen by [blob.ToReadCloser]: every reader it opens
// sends the request.
type opener Blob

func (o *opener) ReadCloser() (io.ReadCloser, error) {
	return (*Blob)(o).open()
}

// open sends the request and returns a reader over its response body. A
// response announcing another size than the learned one is rejected here; one
// without a length is checked against it while it is read.
func (b *Blob) open() (io.ReadCloser, error) {
	b.mu.Lock()
	if b.sent && !b.fetch.repeatable() {
		b.mu.Unlock()
		return nil, fmt.Errorf("download of %s cannot be read again: sending the %s request again may have side effects", b.fetch.safeURL, b.fetch.request.Method)
	}
	b.sent = true
	b.mu.Unlock()

	r, err := b.fetch.clone()
	if err != nil {
		return nil, err
	}
	rep, err := b.fetch.send(r)
	if err != nil {
		return nil, err
	}
	if b.check != nil {
		if err := b.check(rep.resp.Header); err != nil {
			_ = rep.resp.Body.Close()
			return nil, err
		}
	}
	m := b.learn(rep.resp.Header, rep.size)
	if m.size >= 0 && rep.size >= 0 && rep.size != m.size {
		_ = rep.resp.Body.Close()
		return nil, fmt.Errorf("response body from %s announces %d bytes, expected %d", b.fetch.safeURL, rep.size, m.size)
	}
	return &bodyReader{
		blob:     b,
		rep:      rep,
		expected: m.size,
		hashers:  newHashers(b.algorithms),
	}, nil
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
	// expected is the learned content size, or [blob.SizeUnknown].
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
	return r.blob.verify(r.rep.resp.Header, Computed{Content: sums(r.hashers), Pinned: r.blob.pinned()})
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

// maxCloseDrain is how many unread bytes Close reads on to reach the end of a
// partly read body. It covers tails such as tar padding after the
// end-of-archive marker, without downloading the rest of a body the consumer
// abandoned.
const maxCloseDrain = 1 << 20

// Close closes the response body. It also returns the terminal error of the
// reader, so callers that drop the error of the last Read still see it.
//
// A consumer may stop before the end of the body: at the known size without
// seeing EOF (including empty content), or earlier, like a tar reader at the
// end-of-archive marker. Close then reads up to [maxCloseDrain] more bytes so the
// size check and the [Verifier] still run. A body with a verifier that does not
// end within that budget fails Close as not verified. Without a verifier, a
// partly read body is closed unchecked.
func (r *bodyReader) Close() error {
	if r.closed {
		return r.err
	}
	atKnownSize := r.expected >= 0 && r.read == r.expected
	if !r.done && r.err == nil && (atKnownSize || (r.read > 0 && r.blob.verify != nil)) {
		// One byte past the budget, so a tail of exactly the budget still sees EOF.
		if r.expected < 0 || r.expected-r.read <= maxCloseDrain {
			_, _ = io.CopyN(io.Discard, r, maxCloseDrain+1)
		}
		if !r.done && r.err == nil {
			r.err = fmt.Errorf("response body from %s closed after %d bytes, before its end: content not verified", r.blob.fetch.safeURL, r.read)
		}
	}
	r.closed = true
	return errors.Join(r.err, r.rep.resp.Body.Close())
}
