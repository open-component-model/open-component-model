package blob

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
)

// DefaultArchiveBlobBufferSize is the default buffer size used to archive blobs.
// It is slightly larger than the default buffer size used by io.Copy as most blobs
// encountered in practice are larger than the default buffer size.
const DefaultArchiveBlobBufferSize = 128 * 1024 // 128 KiB

// ArchiveBlob archives a ReadOnlyBlob to the tar writer.
// it assumes that size and digest are already known to compute its header.
// The buffer is used to copy the blob data, if nil, a new buffer is allocated.
func ArchiveBlob(name string, size uint64, digest string, b ReadOnlyBlob, writer *tar.Writer, buf []byte) (err error) {
	sizeInt := int64(size)
	if sizeInt < 0 {
		return fmt.Errorf("size %d is negative (overflow)", size)
	}
	if err := writer.WriteHeader(&tar.Header{
		Name: name,
		Mode: 0o644,
		Size: sizeInt,
	}); err != nil {
		return fmt.Errorf("unable to write blob header: %w", err)
	}
	data, err := b.ReadCloser()
	if err != nil {
		return fmt.Errorf("unable to read blob %s: %w", digest, err)
	}
	defer func() {
		err = errors.Join(err, data.Close())
	}()

	if buf == nil {
		buf = make([]byte, DefaultArchiveBlobBufferSize)
	}

	if _, err := io.CopyBuffer(writer, data, buf); err != nil {
		return fmt.Errorf("unable to write blob: %w", err)
	}

	return nil
}
