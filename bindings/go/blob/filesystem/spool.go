package filesystem

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
)

// spoolFilePrefix starts the name of every spool file, where it has one.
const spoolFilePrefix = "ocm-blob-"

// SpooledBlob is a blob held in a temporary file that the operating system frees together
// with its last handle. Its size and digest were recorded while the file was written.
//
// Every reader has its own position. Close the blob once no reader is in use any more.
type SpooledBlob struct {
	file   *os.File
	size   int64
	digest digest.Digest
}

var (
	_ blob.ReadOnlyBlob = (*SpooledBlob)(nil)
	_ blob.SizeAware    = (*SpooledBlob)(nil)
	_ blob.DigestAware  = (*SpooledBlob)(nil)
	_ io.Closer         = (*SpooledBlob)(nil)
)

// Spool copies content into a temporary file in dir and returns it as a blob of known size
// and digest. Both are computed while the file is written, the digest with algorithm
// ([digest.Canonical] if empty), so content is read once and never held in memory. An empty
// dir stands for the default temporary directory of the operating system; a directory that
// does not exist is created.
//
// The file does not outlive the process, not even one that is killed: on unix it is unlinked
// as soon as it is created and never shows up in dir, on Windows it is deleted when it is
// closed. Closing the returned blob frees it earlier.
func Spool(content blob.ReadOnlyBlob, dir string, algorithm digest.Algorithm) (_ *SpooledBlob, err error) {
	if algorithm == "" {
		algorithm = digest.Canonical
	}
	if !algorithm.Available() {
		return nil, fmt.Errorf("digest algorithm %q is not available", algorithm)
	}
	if dir != "" {
		// Do not depend on someone else having created the directory.
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("failed to create spool directory: %w", err)
		}
	}
	file, err := createSpoolFile(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to create spool file: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, file.Close())
		}
	}()

	digester := algorithm.Digester()
	size, err := copyAndClose(io.MultiWriter(file, digester.Hash()), content)
	if err != nil {
		return nil, fmt.Errorf("failed to spool blob to file: %w", err)
	}
	return &SpooledBlob{file: file, size: size, digest: digester.Digest()}, nil
}

// copyAndClose copies src into dst. A source that verifies its content reports a mismatch
// when it is closed, so the close error is part of the result.
func copyAndClose(dst io.Writer, src blob.ReadOnlyBlob) (_ int64, err error) {
	reader, err := src.ReadCloser()
	if err != nil {
		return 0, err
	}
	defer func() {
		err = errors.Join(err, reader.Close())
	}()
	return blob.CopyReader(dst, reader)
}

// ReadCloser returns a reader with its own position that can be rewound. Where the
// operating system can open the file again it is a file of its own. Otherwise it reads
// through the handle the blob was spooled with, at explicit offsets.
func (b *SpooledBlob) ReadCloser() (io.ReadCloser, error) {
	if reopened, err := reopenSpoolFile(b.file); err == nil {
		return reopened, nil
	}
	return b.sharedReader(), nil
}

// sharedReader returns a reader over the one handle of the blob.
func (b *SpooledBlob) sharedReader() io.ReadCloser {
	return sectionReadCloser{io.NewSectionReader(b.file, 0, b.size)}
}

// sectionReadCloser gives a section of a file a Close that leaves the file open. Unlike
// io.NopCloser it keeps Seek and ReadAt visible: HTTP clients look for them to tell that a
// request body can be sent again after a failed attempt.
type sectionReadCloser struct{ *io.SectionReader }

func (sectionReadCloser) Close() error { return nil }

// Size returns the number of bytes spooled.
func (b *SpooledBlob) Size() int64 { return b.size }

// Digest returns the digest of the spooled bytes.
func (b *SpooledBlob) Digest() (string, bool) { return b.digest.String(), true }

// Close frees the file. Readers that share the handle of the blob fail afterwards.
func (b *SpooledBlob) Close() error { return b.file.Close() }
