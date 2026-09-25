package repository

import (
	"errors"
	"fmt"
	"io"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
)

// verifyingBlob wraps a blob.ReadOnlyBlob with the digest its content is expected
// to have.
//
// This exists because filesystem.Blob computes its own digest so verification
// happens against itself that always passes. A verifyingBlob verifies against
// an independent source, which is the component descriptor.
//
// Every reader returned by ReadCloser verifies independently: it errors when the
// content hashes to something else, and when it is only read in part.
//
// Verification is streaming, meaning, the target will already been downloaded by the
// time Verification throws an error. It has to be removed by the caller if that happens.
type verifyingBlob struct {
	base     blob.ReadOnlyBlob
	expected digest.Digest
}

var (
	_ blob.ReadOnlyBlob          = (*verifyingBlob)(nil)
	_ blob.SizeAware             = (*verifyingBlob)(nil)
	_ blob.DigestAware           = (*verifyingBlob)(nil)
	_ blob.MediaTypeAware        = (*verifyingBlob)(nil)
	_ blob.MediaTypeOverrideable = (*verifyingBlob)(nil)
	_ io.Closer                  = (*verifyingBlob)(nil)
)

// newVerifyingBlob returns base wrapped so that its content is compared to `expected`.
//
// It fails if expected is not a digest of an algorithm available at runtime.
func newVerifyingBlob(base blob.ReadOnlyBlob, expected digest.Digest) (*verifyingBlob, error) {
	if err := expected.Validate(); err != nil {
		return nil, fmt.Errorf("invalid expected digest %q: %w", expected, err)
	}
	if !expected.Algorithm().Available() {
		return nil, fmt.Errorf("digest algorithm %q of expected digest %q is not available", expected.Algorithm(), expected)
	}

	return &verifyingBlob{base: base, expected: expected}, nil
}

// ReadCloser returns a reader over the content that verifies it against the
// expected digest. The mismatch surfaces from Read once the content ends, and
// from Close in any case, so a caller that only checks one of the two still gets
// verified.
func (b *verifyingBlob) ReadCloser() (io.ReadCloser, error) {
	rc, err := b.base.ReadCloser()
	if err != nil {
		return nil, err
	}
	size := b.Size()
	return &verifyingReadCloser{
		base:     rc,
		digester: b.expected.Algorithm().Digester(),
		expected: b.expected,
		size:     size,
		complete: size == 0,
	}, nil
}

// Digest forwards to the underlying blob, so it reports what the content IS, not
// what it is expected to be.
func (b *verifyingBlob) Digest() (string, bool) {
	if digestAware, ok := b.base.(blob.DigestAware); ok {
		return digestAware.Digest()
	}
	return "", false
}

// Size forwards to the underlying blob, or reports SizeUnknown if it does not know
// it. Nothing here needs the size: the hash covers whatever is read.
func (b *verifyingBlob) Size() int64 {
	if sizeAware, ok := b.base.(blob.SizeAware); ok {
		return sizeAware.Size()
	}
	return blob.SizeUnknown
}

// MediaType returns the media type of the underlying blob if it has one.
func (b *verifyingBlob) MediaType() (string, bool) {
	if mediaTypeAware, ok := b.base.(blob.MediaTypeAware); ok {
		return mediaTypeAware.MediaType()
	}
	return "", false
}

// SetMediaType forwards to the underlying blob and is a no-op if it does not
// support overriding its media type.
func (b *verifyingBlob) SetMediaType(mediaType string) {
	if overrideable, ok := b.base.(blob.MediaTypeOverrideable); ok {
		overrideable.SetMediaType(mediaType)
	}
}

// Close forwards to the underlying blob so that wrapping does not leak the
// resources it owns, such as a temporary file. It is a no-op if the underlying
// blob is not an io.Closer.
func (b *verifyingBlob) Close() error {
	if closer, ok := b.base.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// verifyingReadCloser hashes everything read through it and compares the result
// against the expected digest.
type verifyingReadCloser struct {
	base     io.ReadCloser
	digester digest.Digester
	expected digest.Digest
	size     int64
	read     int64
	complete bool
}

// Read is a tee reader implementation that will not only error on Close but
// also during Read! Since this a sensitive operation, forgetting to check a Close
// error like _ = x.Close() MUST not be left as a possible loophole for skipping
// verification.
func (v *verifyingReadCloser) Read(p []byte) (int, error) {
	n, err := v.base.Read(p)
	if n > 0 {
		if _, writeErr := v.digester.Hash().Write(p[:n]); writeErr != nil {
			return n, writeErr
		}
		v.read += int64(n)
	}
	// A reader handed the exact size, such as the io.CopyN in blob.Copy, stops before
	// the base can report EOF, so a fully read content of known size counts as complete.
	if errors.Is(err, io.EOF) || (v.size > blob.SizeUnknown && v.read >= v.size) {
		v.complete = true
		if mismatch := v.verify(); mismatch != nil {
			return n, mismatch
		}
	}
	return n, err
}

// Close closes the underlying reader and reports a mismatch, which includes the
// content having been read only in part. The reader is closed either way, so a
// failed verification does not leak what it was reading from.
func (v *verifyingReadCloser) Close() error {
	return errors.Join(v.verify(), v.base.Close())
}

// verify refuses content that has not been read in full before comparing digests.
func (v *verifyingReadCloser) verify() error {
	if !v.complete {
		return fmt.Errorf("digest mismatch: incomplete read for digest %s", v.expected)
	}
	if actual := v.digester.Digest(); actual != v.expected {
		return fmt.Errorf("digest mismatch: expected %s, got %s", v.expected, actual)
	}
	return nil
}
