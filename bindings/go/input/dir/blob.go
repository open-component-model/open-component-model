package dir

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	v1 "ocm.software/open-component-model/bindings/go/input/dir/spec/v1"
)

type InputDirBlob struct {
	*filesystem.Blob
	DirMediaType string
}

func (i InputDirBlob) MediaType() (mediaType string, known bool) {
	return i.DirMediaType, i.DirMediaType != ""
}

var _ interface {
	blob.MediaTypeAware
	blob.SizeAware
	blob.DigestAware
} = (*InputDirBlob)(nil)

func GetV1DirBlob(dir v1.Dir) (blob.ReadOnlyBlob, error) {
	if dir.Path == "" {
		return nil, fmt.Errorf("dir path must not be empty")
	}

	// TODO:
	// - Produce a blob from the directory path
	// - Handle the MimeType and Compress options
	// - Handle the PreserveDir, FollowSymlinks, ExcludeFiles and IncludeFiles options
	data := (blob.ReadOnlyBlob)(nil)

	return data, nil
}
