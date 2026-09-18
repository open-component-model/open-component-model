package blob

import (
	"errors"
	"fmt"
	"io"

	"github.com/opencontainers/go-digest"
)

// VerifyingBlob wraps a ReadOnlyBlob with the digest its content is expected to
// have, and reports that expected digest through DigestAware.
//
// This exists because filesystem.Blob computes its own digest so verification
// happens against itself that always passes. A VerifyingBlob verifies against
// an independent source, which is the component descriptor.
//
// Every reader returned by ReadCloser verifies independently: it errors both when
// the content hashes to something else and when it is only read in part.
// Verification is streaming meaning, the target will already been downloaded by the
// time Verification throws an error. It has to be removed by the caller if that happens.
type VerifyingBlob struct {
	base     ReadOnlyBlob
	expected digest.Digest
}

var (
	_ ReadOnlyBlob          = (*VerifyingBlob)(nil)
	_ SizeAware             = (*VerifyingBlob)(nil)
	_ DigestAware           = (*VerifyingBlob)(nil)
	_ MediaTypeAware        = (*VerifyingBlob)(nil)
	_ MediaTypeOverrideable = (*VerifyingBlob)(nil)
	_ io.Closer             = (*VerifyingBlob)(nil)
)

// NewVerifyingBlob returns base wrapped so that its content is held to `expected`.
// It fails if expected is not a digest of an algorithm available at runtime.
func NewVerifyingBlob(base ReadOnlyBlob, expected digest.Digest) (*VerifyingBlob, error) {
	if err := expected.Validate(); err != nil {
		return nil, fmt.Errorf("invalid expected digest %q: %w", expected, err)
	}
	if !expected.Algorithm().Available() {
		return nil, fmt.Errorf("digest algorithm %q of expected digest %q is not available", expected.Algorithm(), expected)
	}
	return &VerifyingBlob{base: base, expected: expected}, nil
}

// ReadCloser returns a reader over the content that verifies it against the
// expected digest. The mismatch surfaces from Read once the content ends, and
// from Close in any case, so a caller that only checks one of the two still gets
// verified.
func (b *VerifyingBlob) ReadCloser() (io.ReadCloser, error) {
	rc, err := b.base.ReadCloser()
	if err != nil {
		return nil, err
	}
	return &verifyingReadCloser{
		base:     rc,
		digester: b.expected.Algorithm().Digester(),
		expected: b.expected,
	}, nil
}

// Digest returns the expected digest. It is always known, as a VerifyingBlob
// cannot be constructed without one.
func (b *VerifyingBlob) Digest() (string, bool) {
	return b.expected.String(), true
}

// Size returns the size of the underlying blob, or SizeUnknown if it does not know it.
func (b *VerifyingBlob) Size() int64 {
	if sizeAware, ok := b.base.(SizeAware); ok {
		return sizeAware.Size()
	}
	return SizeUnknown
}

// MediaType returns the media type of the underlying blob if it has one.
func (b *VerifyingBlob) MediaType() (string, bool) {
	if mediaTypeAware, ok := b.base.(MediaTypeAware); ok {
		return mediaTypeAware.MediaType()
	}
	return "", false
}

// SetMediaType forwards to the underlying blob and is a no-op if it does not
// support overriding its media type.
func (b *VerifyingBlob) SetMediaType(mediaType string) {
	if overrideable, ok := b.base.(MediaTypeOverrideable); ok {
		overrideable.SetMediaType(mediaType)
	}
}

// Close forwards to the underlying blob so that wrapping does not leak the
// resources it owns, such as a temporary file. It is a no-op if the underlying
// blob is not an io.Closer.
func (b *VerifyingBlob) Close() error {
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
	}
	if errors.Is(err, io.EOF) {
		if mismatch := v.verify(); mismatch != nil {
			return n, mismatch
		}
	}
	return n, err
}

// Close closes the underlying reader and reports a mismatch, which includes the
// content having been read only in part.
func (v *verifyingReadCloser) Close() error {
	return errors.Join(v.base.Close(), v.verify())
}

func (v *verifyingReadCloser) verify() error {
	if actual := v.digester.Digest(); actual != v.expected {
		return fmt.Errorf("digest mismatch: expected %s, got %s", v.expected, actual)
	}
	return nil
}
