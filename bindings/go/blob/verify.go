package blob

import (
	"context"
	"fmt"
	"io"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
)

// VerifyingBlob is a ReadOnlyBlob whose content can be held to a digest taken
// from an independent source, which is the component descriptor.
//
// It exists because a blob that computes its own digest verifies against itself
// and always passes. Repositories return one of these so that the caller can
// decide, at the point where it knows whether verification is wanted, by calling
// [VerifyingBlob.Verify]. Reading the blob does NOT verify it.
//
// A resource that carries no digest still yields a VerifyingBlob, one that reports
// having nothing to verify. Whether that is acceptable is the caller's call.
type VerifyingBlob struct {
	base ReadOnlyBlob

	// desc is the digest and size the content is held to. A zero Digest means there
	// is nothing to hold it to.
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

// NewVerifyingBlob returns base wrapped so that [VerifyingBlob.Verify] holds its
// content to expected.
//
// An empty expected is not an error: it means the resource declares nothing to
// verify against, which is what --skip-reference-digest-processing produces.
//
// It fails if expected cannot be used, or if base does not know its size, because
// [content.VerifyReader] bounds content by the declared size and an unknown size
// would let it read nothing and call that a clean result. Failing here rather than
// from Verify keeps the problem at the point it can be acted on. Resolving the size
// can make base materialize its content.
func NewVerifyingBlob(base ReadOnlyBlob, expected digest.Digest) (*VerifyingBlob, error) {
	if expected == "" {
		return &VerifyingBlob{base: base}, nil
	}
	if err := expected.Validate(); err != nil {
		return nil, fmt.Errorf("invalid expected digest %q: %w", expected, err)
	}
	// Verifier() panics on an algorithm without a registered hash, so this cannot
	// be left to VerifyReader.
	if !expected.Algorithm().Available() {
		return nil, fmt.Errorf("digest algorithm %q of expected digest %q is not available", expected.Algorithm(), expected)
	}

	sizeAware, ok := base.(SizeAware)
	if !ok || sizeAware.Size() < 0 {
		return nil, fmt.Errorf("cannot verify a blob of unknown size against digest %q", expected)
	}

	return &VerifyingBlob{
		base: base,
		desc: ociImageSpecV1.Descriptor{Digest: expected, Size: sizeAware.Size()},
	}, nil
}

// Verify reads the content in full and holds it to the expected digest and size.
// It is the caller's decision when, and whether, to call this.
//
// It reports false with no error when there is nothing to verify, because a
// resource is allowed to carry no digest. Whether that is acceptable is the
// caller's policy, not this type's. Content is read from the start, so calling
// this does not disturb any reader the caller holds.
func (b *VerifyingBlob) Verify(_ context.Context) (verified bool, err error) {
	if b.desc.Digest == "" {
		return false, nil
	}

	rc, err := b.base.ReadCloser()
	if err != nil {
		return false, fmt.Errorf("cannot read content to verify it: %w", err)
	}
	defer func() {
		if closeErr := rc.Close(); closeErr != nil && err == nil {
			verified, err = false, closeErr
		}
	}()

	verifier := content.NewVerifyReader(rc, b.desc)
	if _, err := io.Copy(io.Discard, verifier); err != nil {
		return false, fmt.Errorf("digest mismatch: expected %s: %w", b.desc.Digest, err)
	}
	if err := verifier.Verify(); err != nil {
		return false, fmt.Errorf("digest mismatch: expected %s: %w", b.desc.Digest, err)
	}

	return true, nil
}

// ReadCloser returns a reader over the content. It does NOT verify: that is what
// Verify is for, and doing both would hash everything twice.
func (b *VerifyingBlob) ReadCloser() (io.ReadCloser, error) {
	return b.base.ReadCloser()
}

// Digest forwards to the underlying blob, so it reports what the content IS, not
// what it is expected to be. Reporting the expectation here would let a caller
// that computes a digest from a download read back its own input.
func (b *VerifyingBlob) Digest() (string, bool) {
	if digestAware, ok := b.base.(DigestAware); ok {
		return digestAware.Digest()
	}
	return "", false
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
