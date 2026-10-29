package blob

import (
	"archive/tar"
	"errors"
	"fmt"
)

// DefaultArchiveBlobBufferSize is the default buffer size used to archive blobs.
// It is slightly larger than the default buffer size used by io.Copy as most blobs
// encountered in practice are larger than the default buffer size.
const DefaultArchiveBlobBufferSize = 128 * 1024 // 128 KiB

// ArchiveBlob archives a ReadOnlyBlob to the tar writer.
// it assumes that size and digest are already known to compute its header.
func ArchiveBlob(name string, size int64, digest string, b ReadOnlyBlob, writer *tar.Writer) (err error) {
	if err := writer.WriteHeader(&tar.Header{
		Name: name,
		Mode: 0o644,
		Size: size,
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

	if _, err := CopyReader(writer, data); err != nil {
		return fmt.Errorf("unable to write blob: %w", err)
	}

	return nil
}
