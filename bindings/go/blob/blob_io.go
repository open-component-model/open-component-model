package blob

import (
	"errors"
	"io"

	"github.com/opencontainers/go-digest"
)

// Copy copies the contents of a ReadOnlyBlob to a provided io.Writer, performing optional size and digest checks.
// Copy can redirect to io.CopyN if the blob is SizeAware.
// Copy can verify an open container digest if the blob is DigestAware.
func Copy(dst io.Writer, src ReadOnlyBlob) (err error) {
	var size int64 = SizeUnknown
	if srcSizeAware, ok := src.(SizeAware); ok {
		size = srcSizeAware.Size()
	}

	data, err := src.ReadCloser()
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, data.Close())
	}()

	reader := io.Reader(data)

	if digestAware, ok := src.(DigestAware); ok {
		if digRaw, known := digestAware.Digest(); known {
			var dig digest.Digest
			if dig, err = digest.Parse(digRaw); err != nil {
				return err
			}
			verifier := dig.Verifier()
			reader = io.TeeReader(reader, verifier)
			defer func() {
				if !verifier.Verified() {
					err = errors.Join(err, errors.New("blob digest verification failed"))
				}
			}()
		}
	}

	if size > SizeUnknown {
		_, err = io.CopyN(dst, reader, size)
	} else {
		_, err = io.Copy(dst, reader)
	}

	return err
}
