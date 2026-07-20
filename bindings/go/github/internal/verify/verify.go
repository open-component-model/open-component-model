// Package verify digests and verifies content while it streams, without
// buffering it. It exists for the one-shot streams this binding downloads —
// an HTTP response body serving a GitHub source archive — whose size is
// unknown upfront and whose digest can only be a statement about the whole
// content once the stream has been read to EOF.
//
// # Scope
//
// The package owns a single blob implementation: [VerifiedStreamBlob], a
// single-use [blob.ReadOnlyBlob] over such a stream that hashes the bytes as
// the consumer reads them and verifies an expected digest when the reader is
// closed. Buffered blobs are out of scope — the blob module's inmemory and
// filesystem packages cover those.
//
// # Usage
//
//	b, err := verify.NewVerifiedStreamBlob(resp.Body, expected) // "" = calculate-only
//	if err != nil {
//		return err
//	}
//	reader, err := b.ReadCloser()
//	if err != nil {
//		return err
//	}
//	// read the stream, then Close: Close verifies expected, if any.
//	if err := reader.Close(); err != nil {
//		return err
//	}
//	digest, _ := b.Digest()
package verify

import (
	"errors"
	"fmt"
	"hash"
	"io"
	"sync"
	"sync/atomic"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
)

// ErrMismatchedDigest is returned by closing the blob's reader when the fully
// read content does not hash to the expected digest.
var ErrMismatchedDigest = errors.New("mismatched digest")

// VerifiedStreamBlob is a [blob.ReadOnlyBlob] over a stream that can be served
// exactly once, digesting the bytes on the fly as the consumer reads.
// Verification runs when the consumer closes the reader: only after the
// stream has been fully read is the computed digest a statement about the
// whole content. The blob module's direct.Blob is the wrong tool for such a
// stream — its readers never close the source, while a one-shot stream (e.g.
// an HTTP response body) must be closed to release its connection, and here
// Close is also what verifies.
//
// VerifiedStreamBlob deviates from the [blob.ReadOnlyBlob] contract in one
// documented way: the content is a single-use stream, so ReadCloser can serve
// it exactly once. A second call fails instead of silently returning a
// consumed reader.
//
// The consumer owns the stream's lifetime: it must read and close the
// returned reader, or the underlying source stays open.
type VerifiedStreamBlob struct {
	mu        sync.Mutex
	reader    *verifiedStreamReader
	consumed  bool
	mediaType string
}

var (
	_ blob.ReadOnlyBlob          = (*VerifiedStreamBlob)(nil)
	_ blob.SizeAware             = (*VerifiedStreamBlob)(nil)
	_ blob.DigestAware           = (*VerifiedStreamBlob)(nil)
	_ blob.MediaTypeAware        = (*VerifiedStreamBlob)(nil)
	_ blob.MediaTypeOverrideable = (*VerifiedStreamBlob)(nil)
)

// NewVerifiedStreamBlob wraps r so that the bytes the consumer reads are
// hashed on the fly and verified against expected when the reader is closed.
// An empty expected digest makes the blob calculate-only: the digest becomes
// known once the stream has been fully read, and a partially read stream may
// be abandoned freely. A malformed expected digest is rejected here, so that
// a caller cannot end up silently skipping the verification it asked for.
func NewVerifiedStreamBlob(r io.ReadCloser, expected digest.Digest) (*VerifiedStreamBlob, error) {
	algorithm := digest.SHA256
	if expected != "" {
		if err := expected.Validate(); err != nil {
			return nil, fmt.Errorf("invalid expected digest %q: %w", expected, err)
		}
		algorithm = expected.Algorithm()
	}
	digester := algorithm.Digester()
	return &VerifiedStreamBlob{
		reader: &verifiedStreamReader{
			source:   r,
			digester: digester,
			hash:     digester.Hash(),
			expected: expected,
		},
	}, nil
}

// MediaType returns the media type set via SetMediaType, unknown before then.
func (b *VerifiedStreamBlob) MediaType() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.mediaType, b.mediaType != ""
}

// SetMediaType sets the media type of the blob.
func (b *VerifiedStreamBlob) SetMediaType(mediaType string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.mediaType = mediaType
}

// Size returns [blob.SizeUnknown]: a single-use stream is not buffered
// anywhere it could be measured.
func (b *VerifiedStreamBlob) Size() int64 {
	return blob.SizeUnknown
}

// Digest returns the expected digest when one was set, or the digest computed
// while streaming once the stream has been fully read. Before either point
// the digest is unknown.
func (b *VerifiedStreamBlob) Digest() (string, bool) {
	if b.reader.expected != "" {
		return b.reader.expected.String(), true
	}
	if computed := b.reader.computed.Load(); computed != nil {
		return computed.String(), true
	}
	return "", false
}

// ReadCloser returns the stream, wrapped so that the digest is computed as
// the consumer reads. Closing the reader verifies the expected digest, if
// any. The stream can only be served once; a second call fails.
func (b *VerifiedStreamBlob) ReadCloser() (io.ReadCloser, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.consumed {
		return nil, fmt.Errorf("stream was already consumed: a verified stream blob cannot serve a second reader")
	}
	b.consumed = true
	return b.reader, nil
}

// verifiedStreamReader hashes the source's bytes as they are read and runs
// verification on Close. The source is closed regardless of the verification
// verdict, so a failed verification does not leak it.
type verifiedStreamReader struct {
	source   io.ReadCloser
	digester digest.Digester
	hash     hash.Hash
	expected digest.Digest
	// err latches the first read error, so a failed read (or a failed
	// verification) hands out no further content; io.EOF marks the stream
	// fully read.
	err error
	// computed is finalized once the stream reaches EOF: only then is the
	// hash a digest of the whole content rather than a prefix. It is atomic
	// so Digest may be consulted while another goroutine still reads.
	computed atomic.Pointer[digest.Digest]
}

func (r *verifiedStreamReader) Read(p []byte) (n int, err error) {
	if r.err != nil {
		return 0, r.err
	}
	n, err = r.source.Read(p)
	if n > 0 {
		// hash.Hash writes never return an error.
		_, _ = r.hash.Write(p[:n])
	}
	if err != nil {
		r.err = err
		if errors.Is(err, io.EOF) {
			d := r.digester.Digest()
			r.computed.Store(&d)
		}
	}
	return n, err
}

func (r *verifiedStreamReader) Close() error {
	verifyErr := r.verify()
	if closeErr := r.source.Close(); closeErr != nil && verifyErr == nil {
		return fmt.Errorf("error closing verified stream: %w", closeErr)
	}
	return verifyErr
}

// verify decides the close verdict. Only a stream read to EOF can be judged
// against the expected digest; closing earlier is an error only when it
// leaves an expectation uncheckable, so a calculate-only stream may be
// abandoned at any point.
func (r *verifiedStreamReader) verify() error {
	if r.err != nil && !errors.Is(r.err, io.EOF) {
		return fmt.Errorf("error verifying stream: %w", r.err)
	}
	if r.expected == "" {
		return nil
	}
	if r.err == nil {
		return fmt.Errorf("stream closed before being fully read: cannot verify expected digest %s", r.expected)
	}
	if computed := *r.computed.Load(); computed != r.expected {
		r.err = ErrMismatchedDigest
		return fmt.Errorf("digest mismatch: expected %s, computed %s: %w", r.expected, computed, ErrMismatchedDigest)
	}
	return nil
}
