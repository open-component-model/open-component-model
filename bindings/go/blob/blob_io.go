package blob

import (
	"errors"
	"io"

	"github.com/opencontainers/go-digest"
)

// Copy copies the contents of a ReadOnlyBlob to a provided io.Writer, performing optional size and digest checks.
//
// The function first checks if the source blob is SizeAware and retrieves its size if applicable.
// It then reads the blob's data and ensures it is closed after the operation, even if an error occurs.
//
// If the source blob is DigestAware, the function verifies the blob's digest against the provided data.
// It uses an io.TeeReader to read the data while simultaneously verifying the digest. If the verification fails,
// an error is returned indicating the failure.
//
// Depending on whether the size is known, the function either uses io.CopyN to copy a specific number of bytes
// or io.Copy to copy all available data. Thus, if the data is SizeAware, no buffering is necessary, reducing
// allocations and improving performance.
//
// Parameters:
// - dst: The destination io.Writer where the blob's contents will be copied.
// - src: The ReadOnlyBlob source from which the contents will be read.
//
// Returns:
//   - An error if the copy operation fails, including errors from reading the blob, closing the reader,
//     or failing the digest verification. If the operation is successful, it returns nil.
func Copy(dst io.Writer, src ReadOnlyBlob) (err error) {
	size := SizeUnknown
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
