package download

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"sync"

	"ocm.software/open-component-model/bindings/go/blob"
)

// VerifyFunc verifies the digests computed over a fully-read response body against
// that response's headers. It is called once the body has been read to its end and
// must return a non-nil error to fail the stream on a mismatch. The map is keyed by
// the [DigestAlgorithm.Name] the caller requested via [WithDigestAlgorithms].
type VerifyFunc func(headers http.Header, digests map[string]string) error

// Stream is a lazy, replayable blob that streams an HTTP response body on each
// [Stream.ReadCloser] call by re-issuing the request. No temporary file is created
// and no bytes are buffered: the body is read straight from the network.
//
// Replayability is provided by re-issuing the request, so the blob honours the
// [blob.ReadOnlyBlob] contract that every ReadCloser starts from the beginning.
// The common by-value transfer path reads the blob exactly once, so a single
// request is issued.
//
// Verification is inline and stream-then-verify: when a [VerifyFunc] is set, the
// requested digests are computed as the body is read and verified once the body
// reaches its end (and on Close). The mismatch surfaces from the final Read and
// from Close, so a consumer that streams the body to a target fails a corrupted
// transfer even though the bytes have already been forwarded.
//
// The context captured at construction time governs every request the blob issues;
// a lazy blob has no other place to carry it.
type Stream struct {
	ctx    context.Context
	req    Request
	o      *option
	verify VerifyFunc

	mu        sync.Mutex
	mediaType string
}

var (
	_ blob.ReadOnlyBlob   = (*Stream)(nil)
	_ blob.MediaTypeAware = (*Stream)(nil)
)

// NewStream builds a lazy streaming blob for req. The verify callback is optional;
// when nil no verification runs and no digests are computed unless requested via
// options. Options mirror those of [Download] (client, credentials, max size,
// digest algorithms); [WithTempDir] has no effect because nothing is written to
// disk.
func NewStream(ctx context.Context, req Request, verify VerifyFunc, opts ...Option) *Stream {
	o := &option{}
	for _, opt := range opts {
		opt(o)
	}
	return &Stream{
		ctx:       ctx,
		req:       req,
		o:         o,
		verify:    verify,
		mediaType: req.MediaType,
	}
}

// MediaType reports the configured media type, or the one captured from the first
// response when the request did not pin one. It is unknown until the first read
// when neither is available.
func (s *Stream) MediaType() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mediaType == "" {
		return "", false
	}
	return s.mediaType, true
}

// ReadCloser issues the request and returns a reader over the response body. Each
// call starts a fresh request, so the blob is replayable. The returned reader
// computes the requested digests inline and verifies them once the body ends.
func (s *Stream) ReadCloser() (io.ReadCloser, error) {
	resp, safeURL, err := open(s.ctx, s.req, s.o)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	if s.mediaType == "" {
		if ct := resp.Header.Get("Content-Type"); ct != "" {
			s.mediaType = ct
		}
	}
	s.mu.Unlock()

	maxDownloadSize := DefaultMaxDownloadSize
	if s.o.MaxDownloadSize != nil {
		maxDownloadSize = *s.o.MaxDownloadSize
	}

	// Reject an oversized body before any of it is read when the server announces
	// the size up front. ContentLength is negative when unknown.
	if maxDownloadSize > 0 && resp.ContentLength > maxDownloadSize {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("response body from %s exceeds maximum allowed size of %d bytes", safeURL, maxDownloadSize)
	}

	src := io.Reader(resp.Body)
	if maxDownloadSize > 0 {
		src = io.LimitReader(resp.Body, maxDownloadSize+1)
	}

	hashers := make(map[string]hash.Hash, len(s.o.DigestAlgorithms))
	writers := make([]io.Writer, 0, len(s.o.DigestAlgorithms))
	for _, alg := range s.o.DigestAlgorithms {
		h := alg.New()
		hashers[alg.Name] = h
		writers = append(writers, h)
	}
	if len(writers) > 0 {
		src = io.TeeReader(src, io.MultiWriter(writers...))
	}

	return &streamReader{
		body:    resp.Body,
		src:     src,
		headers: resp.Header,
		hashers: hashers,
		verify:  s.verify,
		maxSize: maxDownloadSize,
		safeURL: safeURL,
	}, nil
}

// streamReader reads a single response body, computes digests inline and runs the
// stream's verification once the body ends. It owns the response body and closes
// it on Close.
type streamReader struct {
	body    io.ReadCloser
	src     io.Reader
	headers http.Header
	hashers map[string]hash.Hash
	verify  VerifyFunc
	maxSize int64
	safeURL string

	read     int64
	finished bool
	finalErr error
}

func (r *streamReader) Read(p []byte) (int, error) {
	n, err := r.src.Read(p)
	if n > 0 {
		r.read += int64(n)
	}
	if errors.Is(err, io.EOF) {
		if vErr := r.finish(); vErr != nil {
			return n, vErr
		}
	}
	return n, err
}

// Close closes the response body and, unless the body was already read to its end,
// runs verification over whatever was read. A partial read fails verification, so a
// caller cannot skip it by closing early.
func (r *streamReader) Close() error {
	vErr := r.finish()
	closeErr := r.body.Close()
	return errors.Join(vErr, closeErr)
}

// finish enforces the size limit and runs verification exactly once, caching the
// outcome so Read and Close report the same result.
func (r *streamReader) finish() error {
	if r.finished {
		return r.finalErr
	}
	r.finished = true

	if r.maxSize > 0 && r.read > r.maxSize {
		r.finalErr = fmt.Errorf("response body from %s exceeds maximum allowed size of %d bytes", r.safeURL, r.maxSize)
		return r.finalErr
	}

	if r.verify == nil {
		return nil
	}

	digests := make(map[string]string, len(r.hashers))
	for name, h := range r.hashers {
		digests[name] = hex.EncodeToString(h.Sum(nil))
	}
	r.finalErr = r.verify(r.headers, digests)
	return r.finalErr
}
