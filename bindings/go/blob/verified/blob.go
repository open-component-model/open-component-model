// Package verified provides a blob whose content is described by metadata known
// up front, such as an OCI descriptor, and verified against it on every read.
package verified

import (
	"errors"
	"fmt"
	"io"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
)

// Metadata describes content without having to introspect it.
type Metadata struct {
	// Digest of the content. Empty if unknown; the digest is then not verified.
	Digest digest.Digest
	// Size of the content in bytes, or blob.SizeUnknown to take the size the
	// source reports, if any. Content is verified against the size the blob
	// reports; an unknown size is not verified.
	Size int64
	// MediaType of the content. Empty to take the media type the source
	// reports, if any.
	MediaType string
}

var (
	_ blob.ReadOnlyBlob   = (*Blob)(nil)
	_ blob.SizeAware      = (*Blob)(nil)
	_ blob.DigestAware    = (*Blob)(nil)
	_ blob.MediaTypeAware = (*Blob)(nil)
)

// Blob reports digest, size and media type from Metadata and verifies the content
// read from its source against them. Size and media type left unknown in the
// Metadata are asked from the source each time, so a source that learns them
// lazily is not forced to do so up front.
//
// The blob holds no open resources: every ReadCloser opens fresh content from the
// source, so it can be read repeatedly and needs no Close as long as the source
// does not either (e.g. a [blob.ReadOnlyBlobFunc] that opens a stream on demand).
//
// Content is always verified against what the blob reports.
type Blob struct {
	src  blob.ReadOnlyBlob
	meta Metadata
}

// New returns a Blob reading from src and verifying it against meta.
func New(src blob.ReadOnlyBlob, meta Metadata) *Blob {
	return &Blob{src: src, meta: meta}
}

// ReadCloser opens the content of the source. The returned reader fails once the
// content turns out not to match the metadata: from Read when the content ends or
// exceeds the expected size, and from Close in any case, including a partial read.
// Close always closes the source reader, also when verification fails.
func (b *Blob) ReadCloser() (io.ReadCloser, error) {
	expected := b.meta.Digest
	var digester digest.Digester
	if expected != "" {
		if err := expected.Validate(); err != nil {
			return nil, fmt.Errorf("invalid expected digest %q: %w", expected, err)
		}
		digester = expected.Algorithm().Digester()
	}
	rc, err := b.src.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("failed to open blob content: %w", err)
	}
	return &reader{base: rc, digester: digester, expected: expected, size: b.Size()}, nil
}

// Digest returns the expected digest, if known.
func (b *Blob) Digest() (string, bool) {
	return b.meta.Digest.String(), b.meta.Digest != ""
}

// Size returns the expected size: the one from the Metadata, otherwise the one
// the source reports, otherwise blob.SizeUnknown.
func (b *Blob) Size() int64 {
	if b.meta.Size != blob.SizeUnknown {
		return b.meta.Size
	}
	if sizeAware, ok := b.src.(blob.SizeAware); ok {
		return sizeAware.Size()
	}
	return blob.SizeUnknown
}

// MediaType returns the media type from the Metadata, otherwise the one the
// source reports, if known.
func (b *Blob) MediaType() (string, bool) {
	if b.meta.MediaType != "" {
		return b.meta.MediaType, true
	}
	if mediaTypeAware, ok := b.src.(blob.MediaTypeAware); ok {
		return mediaTypeAware.MediaType()
	}
	return "", false
}

// reader counts and hashes everything read through it and compares the result
// against the metadata of its blob.
type reader struct {
	base     io.ReadCloser
	digester digest.Digester // nil if the digest is unknown
	expected digest.Digest
	size     int64
	read     int64
	eof      bool
}

// Read also reports a mismatch, not only Close, so that a caller ignoring the
// Close error (_ = rc.Close()) cannot skip verification.
func (r *reader) Read(p []byte) (int, error) {
	n, err := r.base.Read(p)
	if n > 0 {
		r.read += int64(n)
		if r.digester != nil {
			// hash.Hash.Write never returns an error.
			_, _ = r.digester.Hash().Write(p[:n])
		}
	}
	if r.size != blob.SizeUnknown && r.read > r.size {
		return n, r.verify()
	}
	if errors.Is(err, io.EOF) {
		r.eof = true
		if mismatch := r.verify(); mismatch != nil {
			return n, mismatch
		}
	}
	return n, err
}

func (r *reader) Close() error {
	return errors.Join(r.verify(), r.base.Close())
}

// verify refuses content that has not been read in full before comparing digests.
// Content of known size counts as complete once that size is read, because readers
// handed the exact size (e.g. io.CopyN in blob.Copy) stop before the source reports EOF.
func (r *reader) verify() error {
	switch {
	case r.size != blob.SizeUnknown && r.read != r.size:
		return fmt.Errorf("size mismatch: expected %d bytes, read %d", r.size, r.read)
	case r.size == blob.SizeUnknown && !r.eof:
		return errors.New("incomplete read: content of unknown size was not read to its end")
	case r.digester != nil && r.digester.Digest() != r.expected:
		return fmt.Errorf("digest mismatch: expected %s, got %s", r.expected, r.digester.Digest())
	}
	return nil
}
