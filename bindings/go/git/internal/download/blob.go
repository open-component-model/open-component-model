package download

import (
	"io"
	"sync"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

// archiveBlob owns the downloaded file, independently of readers opened on it.
// Closing a reader must not remove the archive: consumers may read it again to
// calculate a digest or upload it. Close releases the archive after all readers
// have been closed.
type archiveBlob struct {
	*filesystem.Blob
	close func() error
}

var _ io.Closer = (*archiveBlob)(nil)

func newArchiveBlob(b *filesystem.Blob, path string) *archiveBlob {
	return &archiveBlob{
		Blob: b,
		close: sync.OnceValue(func() error {
			return removeIgnoringMissing(path)
		}),
	}
}

func (b *archiveBlob) Close() error {
	return b.close()
}
