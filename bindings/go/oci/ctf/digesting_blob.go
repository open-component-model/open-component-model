package ctf

import (
	"errors"
	"fmt"
	"io"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
)

// digestingBlob is a single-use blob over a stream whose digest and size are not known
// up front. It is its own reader: it computes both while being read and reports them
// only once the content has been read to its end and matched the expected values, which
// is when ctf.SaveBlob asks for the digest.
type digestingBlob struct {
	data           io.Reader
	expectedDigest digest.Digest
	expectedSize   int64
	digester       digest.Digester
	size           int64
	opened         bool
	complete       bool
}

var (
	_ blob.DigestAware = (*digestingBlob)(nil)
	_ blob.SizeAware   = (*digestingBlob)(nil)
	_ io.ReadCloser    = (*digestingBlob)(nil)
)

// newDigestingBlob wraps data. A non-empty expectedDigest selects the digest algorithm
// and must match the content; an expectedSize >= 0 must match its length.
func newDigestingBlob(data io.Reader, expectedDigest digest.Digest, expectedSize int64) (*digestingBlob, error) {
	algorithm := digest.Canonical
	if expectedDigest != "" {
		if err := expectedDigest.Validate(); err != nil {
			return nil, fmt.Errorf("invalid expected digest %q: %w", expectedDigest, err)
		}
		algorithm = expectedDigest.Algorithm()
	}
	return &digestingBlob{
		data:           data,
		expectedDigest: expectedDigest,
		expectedSize:   expectedSize,
		digester:       algorithm.Digester(),
	}, nil
}

// ReadCloser returns the blob itself. It can be called only once, as the stream is consumed.
func (b *digestingBlob) ReadCloser() (io.ReadCloser, error) {
	if b.opened {
		return nil, errors.New("streamed blob content can only be read once")
	}
	b.opened = true
	return b, nil
}

// Read hashes and counts the content and verifies it once the content ends, so a
// mismatch fails the read itself rather than relying on the caller to check Close.
func (b *digestingBlob) Read(p []byte) (int, error) {
	n, err := b.data.Read(p)
	b.digester.Hash().Write(p[:n])
	b.size += int64(n)
	if !errors.Is(err, io.EOF) {
		return n, err
	}
	if dig := b.digester.Digest(); b.expectedDigest != "" && dig != b.expectedDigest {
		return n, fmt.Errorf("content digest %s does not match expected digest %s", dig, b.expectedDigest)
	}
	if b.expectedSize >= 0 && b.size != b.expectedSize {
		return n, fmt.Errorf("content size %d does not match expected size %d", b.size, b.expectedSize)
	}
	b.complete = true
	return n, err
}

// Close fails if the content was not read in full and verified.
func (b *digestingBlob) Close() error {
	if !b.complete {
		return errors.New("streamed blob content was not read to its end")
	}
	return nil
}

// Digest reports the digest of the content once it has been read in full and verified.
func (b *digestingBlob) Digest() (string, bool) {
	if !b.complete {
		return "", false
	}
	return b.digester.Digest().String(), true
}

// Size reports the length of the content once it has been read in full and verified.
func (b *digestingBlob) Size() int64 {
	if !b.complete {
		return blob.SizeUnknown
	}
	return b.size
}
