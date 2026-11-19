package integrity

import (
	"context"
	"io"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/verified"
)

var (
	_ blob.ReadOnlyBlob   = (*digestBlob)(nil)
	_ blob.SizeAware      = (*digestBlob)(nil)
	_ blob.DigestAware    = (*digestBlob)(nil)
	_ blob.MediaTypeAware = (*digestBlob)(nil)

	_ blob.ReadOnlyBlob   = (*artifactBlob)(nil)
	_ blob.SizeAware      = (*artifactBlob)(nil)
	_ blob.DigestAware    = (*artifactBlob)(nil)
	_ blob.MediaTypeAware = (*artifactBlob)(nil)
)

// digestBlob binds base to the digest of its raw bytes. It reports that digest without
// reading, so consumers that content-address the blob use the digest it is verified against.
type digestBlob struct {
	base   blob.ReadOnlyBlob
	digest digest.Digest
	// verifyOnRead is false if the content was already hashed and found to match.
	verifyOnRead bool
}

// ReadCloser returns a reader that fails from Read once the content ends, and from
// Close in any case (including a partial read), if the content does not match.
// The size is only asked from base once content arrives, so a lazy source (such as a
// streaming HTTP download) is not probed before it is read.
func (b *digestBlob) ReadCloser() (io.ReadCloser, error) {
	if !b.verifyOnRead {
		return b.base.ReadCloser()
	}
	return verified.New(b.base, verified.Metadata{Digest: b.digest, Size: blob.SizeUnknown}).ReadCloser()
}

func (b *digestBlob) Digest() (string, bool) {
	return b.digest.String(), true
}

func (b *digestBlob) Size() int64 {
	return sizeOf(b.base)
}

func (b *digestBlob) MediaType() (string, bool) {
	return mediaTypeOf(b.base)
}

// artifactBlob binds an archive holding an OCI artifact to the digest of the artifact's
// root. Unlike digestBlob, it reports the digest of the archive itself, because that is
// what the bytes it serves hash to.
type artifactBlob struct {
	// ctx is the context of the Verify call, which bounds every read of the blob.
	ctx      context.Context
	base     blob.ReadOnlyBlob
	root     digest.Digest
	verifier ArtifactVerifier
}

func (b *artifactBlob) ReadCloser() (io.ReadCloser, error) {
	rc, err := b.base.ReadCloser()
	if err != nil {
		return nil, err
	}
	return b.verifier.Verify(b.ctx, rc, b.root, sizeOf(b.base)), nil
}

func (b *artifactBlob) Digest() (string, bool) {
	if aware, ok := b.base.(blob.DigestAware); ok {
		return aware.Digest()
	}
	return "", false
}

func (b *artifactBlob) Size() int64 {
	return sizeOf(b.base)
}

func (b *artifactBlob) MediaType() (string, bool) {
	return mediaTypeOf(b.base)
}

func sizeOf(b blob.ReadOnlyBlob) int64 {
	if aware, ok := b.(blob.SizeAware); ok {
		return aware.Size()
	}
	return blob.SizeUnknown
}

func mediaTypeOf(b blob.ReadOnlyBlob) (string, bool) {
	if aware, ok := b.(blob.MediaTypeAware); ok {
		return aware.MediaType()
	}
	return "", false
}
