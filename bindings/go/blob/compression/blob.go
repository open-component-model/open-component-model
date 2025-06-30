// Package compression provides functionality for handling compressed blobs in the Open Component Model.
// It supports various compression methods and provides utilities for working with compressed data streams.
package compression

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strings"

	"ocm.software/open-component-model/bindings/go/blob"
)

const (
	MediaTypeGzip       = "application/gzip"
	MediaTypeGzipSuffix = "+gzip"
)

// Method represents the type of compression algorithm used for blob compression.
type Method string

const (
	// MethodCanonical is the default compression method used by the package.
	MethodCanonical = MethodGzip

	// MethodGzip represents GZIP compression.
	MethodGzip Method = "gzip"
)

// Compress creates a new compressed Blob with the specified base blob and default compression method.
// The base blob will be compressed using the canonical compression method (GZIP).
func Compress(b blob.ReadOnlyBlob) *Blob {
	return &Blob{ReadOnlyBlob: b, CompressionMethod: MethodCanonical}
}

// Blob represents a compressed blob that wraps a base ReadOnlyBlob.
// It implements the MediaTypeAware interface and provides compression functionality.
type Blob struct {
	blob.ReadOnlyBlob

	CompressionMethod Method
}

// MediaType returns the media type of the compressed blob.
// It appends the appropriate compression suffix to the base blob's media type.
// Returns the media type and true if the media type is known.
func (b *Blob) MediaType() (mediaType string, known bool) {
	return mediaTypeForBlob(b.ReadOnlyBlob, b.CompressionMethod), true
}

// mediaTypeForBlob determines the media type for a blob based on its compression method.
// It handles different compression methods and returns the appropriate media type string.
func mediaTypeForBlob(b blob.ReadOnlyBlob, method Method) string {
	var mediaType string

	switch method {
	case MethodGzip:
		fallthrough
	default:
		mediaType = getMediaType(b, MediaTypeGzipSuffix, MediaTypeGzip)
	}

	return mediaType
}

// getMediaType determines the media type for a blob, considering its compression.
// If the blob implements MediaTypeAware, it uses that media type and appends the compression suffix.
// Otherwise, it returns the default media type for the compression method.
func getMediaType(b blob.ReadOnlyBlob, ext, def string) string {
	var mediaType string
	if mediaTypeAware, ok := b.(blob.MediaTypeAware); ok {
		if mediaType, ok = mediaTypeAware.MediaType(); ok {
			mediaType += ext
		}
	}

	if mediaType == "" {
		mediaType = def
	}

	return mediaType
}

// ReadCloser returns an io.ReadCloser that provides access to the compressed blob data.
// The returned reader will automatically compress the data from the base blob using the specified compression method.
// The caller is responsible for closing the returned ReadCloser.
func (b *Blob) ReadCloser() (io.ReadCloser, error) {
	base, err := b.ReadOnlyBlob.ReadCloser()
	if err != nil {
		return nil, err
	}

	reader, writer := io.Pipe()

	go compress(base, writer, b.CompressionMethod)

	return reader, nil
}

// compress compresses the data from the reader with the specified compression method and writes it to the writer.
func compress(reader io.ReadCloser, writer *io.PipeWriter, method Method) {
	var compressed io.WriteCloser

	switch method {
	case MethodGzip:
		fallthrough
	default:
		compressed = gzip.NewWriter(writer)
	}

	_, err := io.Copy(compressed, reader)
	writer.CloseWithError(errors.Join(err, compressed.Close(), reader.Close()))
}

// Decompress creates a decompressed version of the given blob if it is compressed.
// It detects the compression method based on the blob's media type and returns a new
// blob that provides access to the decompressed data. If the blob is not compressed,
// it returns the original blob unchanged.
//
// The function supports GZIP compression and handles both standalone GZIP files
// (MediaTypeGzip) and compressed content with MediaTypeGzipSuffix suffix.
//
// Returns:
//   - A ReadOnlyBlob that provides access to the decompressed data
//   - An error if the blob cannot be decompressed
func Decompress(b blob.ReadOnlyBlob) (blob.ReadOnlyBlob, error) {
	var (
		method    Method
		mediaType string
	)

	if mediaTypeAware, ok := b.(blob.MediaTypeAware); ok {
		if mediaType, ok = mediaTypeAware.MediaType(); ok {
			if isGzip := mediaType == MediaTypeGzip || strings.HasSuffix(mediaType, MediaTypeGzipSuffix); isGzip {
				method = MethodGzip

				if mediaType == MediaTypeGzip {
					mediaType = "application/octet-stream"
				}

				mediaType = strings.TrimSuffix(mediaType, MediaTypeGzipSuffix)
			}
		}
	}

	if method == "" {
		// we can return the data as is because it is not knowingly compressed
		return b, nil
	}

	return &DecompressedBlob{
		ReadOnlyBlob:      b,
		compressionMethod: method,
		mediaType:         mediaType,
	}, nil
}

// DecompressedBlob represents a blob that has been decompressed from its original compressed form.
// It wraps the original compressed blob and provides transparent access to the decompressed data.
type DecompressedBlob struct {
	blob.ReadOnlyBlob

	compressionMethod Method
	mediaType         string
}

// MediaType returns the media type of the decompressed blob.
// For GZIP compressed blobs, it removes the "+gzip" suffix or changes "application/gzip"
// to "application/octet-stream" to indicate the decompressed content type.
func (d *DecompressedBlob) MediaType() (string, bool) {
	return d.mediaType, true
}

// ReadCloser returns an io.ReadCloser that provides access to the decompressed blob data.
// The returned reader will automatically decompress the data from the base blob using
// the appropriate decompression method. The caller is responsible for closing the returned ReadCloser.
//
// Returns:
//   - An io.ReadCloser that provides access to the decompressed data
//   - An error if the decompression fails or if the compressed data cannot be read
func (d *DecompressedBlob) ReadCloser() (io.ReadCloser, error) {
	data, err := d.ReadOnlyBlob.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("error reading compressed blob: %w", err)
	}

	var decompressed io.ReadCloser

	switch d.compressionMethod {
	case MethodGzip:
		fallthrough
	default:
		gzReader, err := gzip.NewReader(data)
		if err != nil {
			return nil, fmt.Errorf("error creating gzip reader: %w", err)
		}

		decompressed = gzReader
	}

	return struct {
		io.Reader
		io.Closer
	}{
		Reader: decompressed,
		Closer: closerFunc(func() error {
			return errors.Join(decompressed.Close(), data.Close())
		}),
	}, nil
}

type closerFunc func() error

func (f closerFunc) Close() error {
	return f()
}
