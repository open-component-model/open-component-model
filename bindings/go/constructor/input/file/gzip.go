package file

import (
	"compress/gzip"
	"errors"
	"io"

	"ocm.software/open-component-model/bindings/go/blob"
)

func NewCompressedBlob(b blob.ReadOnlyBlob) *CompressedBlob {
	var mediaType string
	if mediaTypeAware, ok := b.(blob.MediaTypeAware); ok {
		if mediaType, ok = mediaTypeAware.MediaType(); ok {
			mediaType += "+gzip"
		}
	}
	if mediaType == "" {
		mediaType = "application/gzip"
	}
	return &CompressedBlob{ReadOnlyBlob: b, mediaType: mediaType}
}

type CompressedBlob struct {
	blob.ReadOnlyBlob
	mediaType string
}

func (b *CompressedBlob) MediaType() (mediaType string, known bool) {
	return b.mediaType, true
}

func (b *CompressedBlob) ReadCloser() (io.ReadCloser, error) {
	base, err := b.ReadOnlyBlob.ReadCloser()
	if err != nil {
		return nil, err
	}

	reader, writer := io.Pipe()

	var compressed io.WriteCloser = gzip.NewWriter(writer)

	go func() {
		_, err := io.Copy(compressed, base)
		writer.CloseWithError(errors.Join(err, compressed.Close(), base.Close()))
	}()

	return reader, nil
}

var _ blob.MediaTypeAware = (*CompressedBlob)(nil)
