package file

import (
	"fmt"

	"github.com/gabriel-vasile/mimetype"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/compression"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	v1 "ocm.software/open-component-model/bindings/go/constructor/input/file/spec/v1"
)

// InputFileBlob wraps a blob and provides an additional media type for interpretation of the file content.
type InputFileBlob struct {
	*filesystem.Blob
	FileMediaType string
}

func (i InputFileBlob) MediaType() (mediaType string, known bool) {
	return i.FileMediaType, i.FileMediaType != ""
}

var _ interface {
	blob.MediaTypeAware
	blob.SizeAware
	blob.DigestAware
} = (*InputFileBlob)(nil)

func GetV1FileBlob(file v1.File) (blob.ReadOnlyBlob, error) {
	if file.Path == "" {
		return nil, fmt.Errorf("file path must not be empty")
	}

	b, err := filesystem.GetBlobFromOSPath(file.Path)
	if err != nil {
		return nil, err
	}

	mediaType := file.MediaType
	if mediaType == "" {
		// see https://github.com/gabriel-vasile/mimetype/blob/master/supported_mimes.md for supported types
		mime, _ := mimetype.DetectFile(file.Path)
		mediaType = mime.String()
	}

	data := blob.ReadOnlyBlob(&InputFileBlob{b, mediaType})

	if file.Compress {
		data = compression.Compress(data)
	}

	return data, nil
}
