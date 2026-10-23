package tar

import (
	"errors"
	"io/fs"
	"path"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// artifactSetFS reads an OCM v1 artifact set like an OCI image layout. Besides its index and
// layout file, an artifact set stores blobs flat as blobs/<algorithm>.<encoded> instead of
// blobs/<algorithm>/<encoded>; OCM v1 keeps OCI artifacts in local blobs in this format.
type artifactSetFS struct {
	fs.FS
}

func (a artifactSetFS) Open(name string) (fs.File, error) {
	f, err := a.FS.Open(name)
	if !errors.Is(err, fs.ErrNotExist) {
		return f, err
	}
	algorithmDir, encoded := path.Split(name)
	if path.Dir(path.Clean(algorithmDir)) != ociImageSpecV1.ImageBlobsDir {
		return nil, err
	}
	return a.FS.Open(path.Join(ociImageSpecV1.ImageBlobsDir, path.Base(algorithmDir)+"."+encoded))
}
