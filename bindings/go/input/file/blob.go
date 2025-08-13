package file

import (
	"fmt"

	"github.com/gabriel-vasile/mimetype"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/compression"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	v1 "ocm.software/open-component-model/bindings/go/input/file/spec/v1"
)

// InputFileBlob wraps a filesystem blob and provides an additional media type for interpretation of the file content.
// It implements the MediaTypeAware, SizeAware, and DigestAware interfaces to provide metadata about the file.
// The FileMediaType field allows for explicit media type specification, which is useful when the automatic
// detection doesn't match the expected type or when working with custom file formats.
type InputFileBlob struct {
	*filesystem.Blob
	FileMediaType string
}

// MediaType returns the media type of the file and whether it is known.
// If FileMediaType is set, it returns that value with known=true.
// If FileMediaType is empty, it returns an empty string with known=false.
func (i InputFileBlob) MediaType() (mediaType string, known bool) {
	return i.FileMediaType, i.FileMediaType != ""
}

var _ interface {
	blob.MediaTypeAware
	blob.SizeAware
	blob.DigestAware
} = (*InputFileBlob)(nil)

// GetV1FileBlob creates a ReadOnlyBlob from a v1.File specification.
// It reads the file from the filesystem, optionally detects the media type if not provided,
// and applies compression if requested. The function returns an error if the file path
// is empty or if there are issues reading the file from the filesystem.
//
// The function performs the following steps:
//  1. Validates that the file path is not empty
//  2. Reads the file from the filesystem using filesystem.GetBlobInWorkingDirectory
//  3. Detects the media type using mimetype.DetectFile if not explicitly provided
//  4. Wraps the blob with InputFileBlob to provide media type awareness
//  5. Applies gzip compression with [compression.Compress] if the Compress flag is set
func GetV1FileBlob(file v1.File, workingDirectory string) (blob.ReadOnlyBlob, error) {
	if file.Path == "" {
		return nil, fmt.Errorf("file path must not be empty")
	}

	b, err := filesystem.GetBlobInWorkingDirectory(file.Path, workingDirectory)
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
