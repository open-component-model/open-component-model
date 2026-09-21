package blob

import (
	"errors"
	"fmt"
	"io"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
)

// VerifyingBlob wraps a ReadOnlyBlob with the digest its content is expected to
// have, and reports that expected digest through DigestAware.
//
// This exists because filesystem.Blob computes its own digest so verification
// happens against itself that always passes. A VerifyingBlob verifies against
// an independent source, which is the component descriptor.
//
// The check itself is [content.VerifyReader], so content is held to the same
// size and digest rules an OCI registry applies. Every reader returned by
// ReadCloser verifies independently: it errors when the content hashes to
// something else, when it is longer than the declared size, and when it is only
// read in part.
//
// Verification is streaming meaning, the target will already been downloaded by the
// time Verification throws an error. It has to be removed by the caller if that happens.
type VerifyingBlob struct {
	base ReadOnlyBlob
	desc ociImageSpecV1.Descriptor
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
//
// It fails if expected is not a digest of an algorithm available at runtime, and
// if base does not know its size: [content.VerifyReader] bounds the content by the
// declared size, and an unknown size would let it read nothing and report that as
// a clean result. Resolving the size here can make base materialize its content.
func NewVerifyingBlob(base ReadOnlyBlob, expected digest.Digest) (*VerifyingBlob, error) {
	if err := expected.Validate(); err != nil {
		return nil, fmt.Errorf("invalid expected digest %q: %w", expected, err)
	}
	if !expected.Algorithm().Available() {
		return nil, fmt.Errorf("digest algorithm %q of expected digest %q is not available", expected.Algorithm(), expected)
	}

	sizeAware, ok := base.(SizeAware)
	if !ok {
		return nil, fmt.Errorf("cannot verify a blob of unknown size against digest %q", expected)
	}
	size := sizeAware.Size()
	if size < 0 {
		return nil, fmt.Errorf("cannot verify a blob of unknown size against digest %q", expected)
	}

	return &VerifyingBlob{
		base: base,
		desc: ociImageSpecV1.Descriptor{Digest: expected, Size: size},
	}, nil
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
		verifier: content.NewVerifyReader(rc, b.desc),
		expected: b.desc.Digest,
	}, nil
}

// Digest returns the expected digest. It is always known, as a VerifyingBlob
// cannot be constructed without one.
func (b *VerifyingBlob) Digest() (string, bool) {
	return b.desc.Digest.String(), true
}

// Size returns the size the content is held to, which is always known.
func (b *VerifyingBlob) Size() int64 {
	return b.desc.Size
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

// verifyingReadCloser drives a [content.VerifyReader] over the content and closes
// what is underneath it.
type verifyingReadCloser struct {
	base     io.ReadCloser
	verifier *content.VerifyReader
	expected digest.Digest
}

// Read reports a mismatch from the read itself, not only from Close.
// [content.VerifyReader] leaves the check to Verify, and since this is a sensitive
// operation, forgetting to check a Close error like _ = x.Close() MUST not be left
// as a possible loophole for skipping verification.
func (v *verifyingReadCloser) Read(p []byte) (int, error) {
	n, err := v.verifier.Read(p)
	if errors.Is(err, io.EOF) {
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

// verify names the failure and keeps the verifier's own error as detail, which is
// what distinguishes content that hashed wrong from content of the wrong length.
func (v *verifyingReadCloser) verify() error {
	if err := v.verifier.Verify(); err != nil {
		return fmt.Errorf("digest mismatch: expected %s: %w", v.expected, err)
	}
	return nil
}
